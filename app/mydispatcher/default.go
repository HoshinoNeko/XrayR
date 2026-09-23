package mydispatcher

//go:generate go run github.com/xtls/xray-core/common/errors/errorgen

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/dns"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/routing"
	routingSession "github.com/xtls/xray-core/features/routing/session"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/pipe"

	"github.com/HoshinoNeko/XrayR/common/limiter"
	"github.com/HoshinoNeko/XrayR/common/rule"
)

var errSniffingTimeout = newError("timeout on sniffing")

type cachedReader struct {
	sync.Mutex
	reader  buf.Reader
	cache   buf.MultiBuffer
	pending chan readResult
	readErr error
	done    chan struct{}
	stopped bool
}

type readResult struct {
	data buf.MultiBuffer
	err  error
}

func (r *cachedReader) Cache(b *buf.Buffer) {
	mb, err := r.readTimeout(time.Millisecond * 100)
	r.Lock()
	if err != buf.ErrReadTimeout {
		r.readErr = err
	}
	if !mb.IsEmpty() {
		r.cache, _ = buf.MergeMulti(r.cache, mb)
	}
	b.Clear()
	rawBytes := b.Extend(buf.Size)
	n := r.cache.Copy(rawBytes)
	b.Resize(0, int32(n))
	r.Unlock()
}

func (r *cachedReader) readInternal() buf.MultiBuffer {
	r.Lock()
	defer r.Unlock()

	if r.cache != nil && !r.cache.IsEmpty() {
		mb := r.cache
		r.cache = nil
		return mb
	}

	return nil
}

func (r *cachedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb := r.readInternal()
	if mb != nil {
		return mb, nil
	}

	if r.readErr != nil {
		return nil, r.readErr
	}
	if r.pending != nil {
		select {
		case result := <-r.pending:
			r.pending = nil
			return result.data, result.err
		case <-r.done:
			return nil, newError("reader interrupted")
		}
	}
	return r.reader.ReadMultiBuffer()
}

func (r *cachedReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb := r.readInternal()
	if mb != nil {
		return mb, nil
	}

	return r.readTimeout(timeout)
}

// Keep one read in flight across sniffing timeouts; never lose bytes or start
// concurrent reads on QUIC streams.
func (r *cachedReader) readTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	if r.pending == nil {
		r.Lock()
		if r.done == nil {
			r.done = make(chan struct{})
		}
		done := r.done
		stopped := r.stopped
		r.Unlock()
		if stopped {
			return nil, newError("reader interrupted")
		}
		r.pending = make(chan readResult)
		pending := r.pending
		go func() {
			data, err := r.reader.ReadMultiBuffer()
			select {
			case pending <- readResult{data, err}:
			case <-done:
				buf.ReleaseMulti(data)
			}
		}()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-r.pending:
		r.pending = nil
		return result.data, result.err
	case <-timer.C:
		return nil, buf.ErrReadTimeout
	case <-r.done:
		return nil, newError("reader interrupted")
	}
}

func (r *cachedReader) Interrupt() {
	r.Lock()
	if !r.stopped {
		r.stopped = true
		if r.done != nil {
			close(r.done)
		}
	}
	if r.cache != nil {
		r.cache = buf.ReleaseMulti(r.cache)
	}
	r.Unlock()
	common.Interrupt(r.reader)
}

// DefaultDispatcher is a default implementation of Dispatcher.
type DefaultDispatcher struct {
	ohm         outbound.Manager
	router      routing.Router
	policy      policy.Manager
	stats       stats.Manager
	dns         dns.Client
	fdns        dns.FakeDNSEngine
	Limiter     *limiter.Limiter
	RuleManager *rule.Manager
}

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		d := new(DefaultDispatcher)
		if err := core.RequireFeatures(ctx, func(om outbound.Manager, router routing.Router, pm policy.Manager, sm stats.Manager, dc dns.Client) error {
			core.OptionalFeatures(ctx, func(fdns dns.FakeDNSEngine) {
				d.fdns = fdns
			})
			return d.Init(config.(*Config), om, router, pm, sm, dc)
		}); err != nil {
			return nil, err
		}
		return d, nil
	}))
}

// Init initializes DefaultDispatcher.
func (d *DefaultDispatcher) Init(config *Config, om outbound.Manager, router routing.Router, pm policy.Manager, sm stats.Manager, dns dns.Client) error {
	d.ohm = om
	d.router = router
	d.policy = pm
	d.stats = sm
	d.Limiter = limiter.New()
	d.RuleManager = rule.New()
	d.dns = dns
	return nil
}

// Type implements common.HasType.
func (*DefaultDispatcher) Type() interface{} {
	return routing.DispatcherType()
}

// Start implements common.Runnable.
func (*DefaultDispatcher) Start() error {
	return nil
}

// Close implements common.Closable.
func (*DefaultDispatcher) Close() error {
	return nil
}

func (d *DefaultDispatcher) getLink(ctx context.Context) (*transport.Link, *transport.Link, error) {
	opt := pipe.OptionsFromContext(ctx)
	uplinkReader, uplinkWriter := pipe.New(opt...)
	downlinkReader, downlinkWriter := pipe.New(opt...)

	inboundLink := &transport.Link{
		Reader: downlinkReader,
		Writer: uplinkWriter,
	}

	outboundLink := &transport.Link{
		Reader: uplinkReader,
		Writer: downlinkWriter,
	}

	sessionInbound := session.InboundFromContext(ctx)
	var user *protocol.MemoryUser
	if sessionInbound != nil {
		user = sessionInbound.User
	}

	if user != nil && len(user.Email) > 0 {
		managed := !d.Limiter.IsStaticInbound(sessionInbound.Tag)
		if managed {
			// Speed Limit and Device Limit
			ip := sessionInbound.Source.Address.String()
			_, _, reject := d.Limiter.GetUserBucket(sessionInbound.Tag, user.Email, ip)
			if reject {
				errors.LogWarning(ctx, "Devices reach the limit: ", user.Email)
				common.Close(outboundLink.Writer)
				common.Close(inboundLink.Writer)
				common.Interrupt(outboundLink.Reader)
				common.Interrupt(inboundLink.Reader)
				return nil, nil, newError("Devices reach the limit: ", user.Email)
			}
			inboundLink.Writer = d.Limiter.UserWriter(inboundLink.Writer, sessionInbound.Tag, user.Email, ip)
			outboundLink.Writer = d.Limiter.UserWriter(outboundLink.Writer, sessionInbound.Tag, user.Email, ip)
		}

		p := d.policy.ForLevel(user.Level)
		if p.Stats.UserUplink {
			name := "user>>>" + user.Email + ">>>traffic>>>uplink"
			if c, _ := d.stats.GetOrRegisterCounter(name); c != nil {
				inboundLink.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  inboundLink.Writer,
				}
			}
		}
		if p.Stats.UserDownlink {
			name := "user>>>" + user.Email + ">>>traffic>>>downlink"
			if c, _ := d.stats.GetOrRegisterCounter(name); c != nil {
				outboundLink.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  outboundLink.Writer,
				}
			}
		}
		if managed {
			allowed := d.Limiter.Permission(sessionInbound.Tag, user.Email)
			inboundLink.Writer = &AuthorizedWriter{Allowed: allowed, Writer: inboundLink.Writer}
			outboundLink.Writer = &AuthorizedWriter{Allowed: allowed, Writer: outboundLink.Writer}
		}
	}

	return inboundLink, outboundLink, nil
}

func (d *DefaultDispatcher) shouldOverride(ctx context.Context, result SniffResult, request session.SniffingRequest, destination net.Destination) bool {
	domain := result.Domain()
	if request.ExcludeForDomain != nil && request.ExcludeForDomain.MatchAny(strings.ToLower(domain)) {
		return false
	}
	protocolString := result.Protocol()
	if resComp, ok := result.(SnifferResultComposite); ok {
		protocolString = resComp.ProtocolForDomainResult()
	}
	for _, p := range request.OverrideDestinationForProtocol {
		if strings.HasPrefix(protocolString, p) || strings.HasPrefix(p, protocolString) {
			return true
		}
		if fkr0, ok := d.fdns.(dns.FakeDNSEngineRev0); ok && protocolString != "bittorrent" && p == "fakedns" &&
			destination.Address.Family().IsIP() && fkr0.IsIPInIPPool(destination.Address) {
			errors.LogInfo(ctx, "Using sniffer ", protocolString, " since the fake DNS missed")
			return true
		}
		if resultSubset, ok := result.(SnifferIsProtoSubsetOf); ok {
			if resultSubset.IsProtoSubsetOf(p) {
				return true
			}
		}
	}

	return false
}

// Dispatch implements routing.Dispatcher.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, destination net.Destination) (*transport.Link, error) {
	if !destination.IsValid() {
		panic("Dispatcher: Invalid destination.")
	}
	outbounds := session.OutboundsFromContext(ctx)
	if len(outbounds) == 0 {
		outbounds = []*session.Outbound{{}}
		ctx = session.ContextWithOutbounds(ctx, outbounds)
	}
	ob := outbounds[len(outbounds)-1]
	ob.OriginalTarget = destination
	ob.Target = destination
	content := session.ContentFromContext(ctx)
	if content == nil {
		content = new(session.Content)
		ctx = session.ContextWithContent(ctx, content)
	}

	sniffingRequest := content.SniffingRequest
	inbound, outbound, err := d.getLink(ctx)
	if err != nil {
		return nil, err
	}
	if !sniffingRequest.Enabled {
		go d.routedDispatch(ctx, outbound, destination)
	} else {
		go func() {
			cReader := &cachedReader{
				reader: outbound.Reader,
			}
			outbound.Reader = cReader
			result, err := sniffer(ctx, cReader, sniffingRequest.MetadataOnly, destination.Network)
			if err == nil {
				content.Protocol = result.Protocol()
			}
			if err == nil && d.shouldOverride(ctx, result, sniffingRequest, destination) {
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				if sniffingRequest.RouteOnly && result.Protocol() != "fakedns" {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			}
			d.routedDispatch(ctx, outbound, destination)
		}()
	}
	return inbound, nil
}

// DispatchLink implements routing.Dispatcher.
func (d *DefaultDispatcher) DispatchLink(ctx context.Context, destination net.Destination, outbound *transport.Link) error {
	if !destination.IsValid() {
		return newError("Dispatcher: Invalid destination.")
	}
	if err := d.decorateDispatchLink(ctx, outbound); err != nil {
		return err
	}
	outbounds := session.OutboundsFromContext(ctx)
	if len(outbounds) == 0 {
		outbounds = []*session.Outbound{{}}
		ctx = session.ContextWithOutbounds(ctx, outbounds)
	}
	ob := outbounds[len(outbounds)-1]
	ob.OriginalTarget = destination
	ob.Target = destination
	content := session.ContentFromContext(ctx)
	if content == nil {
		content = new(session.Content)
		ctx = session.ContextWithContent(ctx, content)
	}
	sniffingRequest := content.SniffingRequest
	if !sniffingRequest.Enabled {
		d.routedDispatch(ctx, outbound, destination)
	} else {
		func() {
			cReader := &cachedReader{
				reader: outbound.Reader,
			}
			outbound.Reader = cReader
			result, err := sniffer(ctx, cReader, sniffingRequest.MetadataOnly, destination.Network)
			if err == nil {
				content.Protocol = result.Protocol()
			}
			if err == nil && d.shouldOverride(ctx, result, sniffingRequest, destination) {
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				if sniffingRequest.RouteOnly && result.Protocol() != "fakedns" {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			}
			d.routedDispatch(ctx, outbound, destination)
		}()
	}

	return nil
}

// decorateDispatchLink applies the same per-user accounting and limiting used by
// Dispatch to links supplied directly by inbounds such as Hysteria 2.
func (d *DefaultDispatcher) decorateDispatchLink(ctx context.Context, link *transport.Link) error {
	inbound := session.InboundFromContext(ctx)
	if inbound == nil || inbound.User == nil || inbound.User.Email == "" {
		return nil
	}
	managed := !d.Limiter.IsStaticInbound(inbound.Tag)
	if managed {
		if auth, ok := inbound.User.Account.(*account.MemoryAccount); ok &&
			!d.Limiter.MatchesUUID(inbound.Tag, inbound.User.Email, auth.Auth) {
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return newError("Hysteria2 authorization has changed")
		}

		ip := ""
		if inbound.Source.IsValid() {
			ip = inbound.Source.Address.String()
		}
		_, _, reject := d.Limiter.GetUserBucket(inbound.Tag, inbound.User.Email, ip)
		if reject {
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return newError("devices reach the limit: ", inbound.User.Email)
		}
		link.Reader = d.Limiter.UserReader(link.Reader, inbound.Tag, inbound.User.Email, ip)
		link.Writer = d.Limiter.UserWriter(link.Writer, inbound.Tag, inbound.User.Email, ip)
	}

	p := d.policy.ForLevel(inbound.User.Level)
	if p.Stats.UserUplink {
		name := "user>>>" + inbound.User.Email + ">>>traffic>>>uplink"
		if counter, _ := d.stats.GetOrRegisterCounter(name); counter != nil {
			link.Reader = &SizeStatReader{Counter: counter, Reader: link.Reader}
		}
	}
	if p.Stats.UserDownlink {
		name := "user>>>" + inbound.User.Email + ">>>traffic>>>downlink"
		if counter, _ := d.stats.GetOrRegisterCounter(name); counter != nil {
			link.Writer = &SizeStatWriter{Counter: counter, Writer: link.Writer}
		}
	}
	if managed {
		allowed := d.Limiter.Permission(inbound.Tag, inbound.User.Email)
		link.Reader = &AuthorizedReader{Allowed: allowed, Reader: link.Reader}
		link.Writer = &AuthorizedWriter{Allowed: allowed, Writer: link.Writer}
	}
	return nil
}

func sniffer(ctx context.Context, cReader *cachedReader, metadataOnly bool, network net.Network) (SniffResult, error) {
	payload := buf.New()
	defer payload.Release()

	sniffer := NewSniffer(ctx)

	metaresult, metadataErr := sniffer.SniffMetadata(ctx)

	if metadataOnly {
		return metaresult, metadataErr
	}

	contentResult, contentErr := func() (SniffResult, error) {
		totalAttempt := 0
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				totalAttempt++
				if totalAttempt > 2 {
					return nil, errSniffingTimeout
				}

				cReader.Cache(payload)
				if !payload.IsEmpty() {
					result, err := sniffer.Sniff(ctx, payload.Bytes(), network)
					if err != common.ErrNoClue {
						return result, err
					}
				}
				if payload.IsFull() {
					return nil, errUnknownContent
				}
			}
		}
	}()
	if contentErr != nil && metadataErr == nil {
		return metaresult, nil
	}
	if contentErr == nil && metadataErr == nil {
		return CompositeResult(metaresult, contentResult), nil
	}
	return contentResult, contentErr
}

func (d *DefaultDispatcher) routedDispatch(ctx context.Context, link *transport.Link, destination net.Destination) {
	var handler outbound.Handler

	// Check if domain and protocol hit the rule
	sessionInbound := session.InboundFromContext(ctx)
	// Whether the inbound connection contains a user
	if sessionInbound != nil && sessionInbound.User != nil {
		if d.RuleManager.Detect(sessionInbound.Tag, destination.String(), sessionInbound.User.Email) {
			errors.LogError(ctx, fmt.Sprintf("User %s access %s reject by rule", sessionInbound.User.Email, destination.String()))
			newError("destination is reject by rule")
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return
		}
	}

	routingLink := routingSession.AsRoutingContext(ctx)
	inTag := routingLink.GetInboundTag()
	isPickRoute := 0
	if forcedOutboundTag := session.GetForcedOutboundTagFromContext(ctx); forcedOutboundTag != "" {
		ctx = session.SetForcedOutboundTagToContext(ctx, "")
		if h := d.ohm.GetHandler(forcedOutboundTag); h != nil {
			isPickRoute = 1
			errors.LogInfo(ctx, "taking platform initialized detour [", forcedOutboundTag, "] for [", destination, "]")
			handler = h
		} else {
			errors.LogError(ctx, "non existing tag for platform initialized detour: ", forcedOutboundTag)
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return
		}
	} else if d.router != nil {
		if route, err := d.router.PickRoute(routingLink); err == nil {
			outTag := route.GetOutboundTag()
			if h := d.ohm.GetHandler(outTag); h != nil {
				isPickRoute = 2
				errors.LogInfo(ctx, "taking detour [", outTag, "] for [", destination, "]")
				handler = h
			} else {
				errors.LogWarning(ctx, "non existing outTag: ", outTag)
			}
		} else {
			errors.LogInfo(ctx, "default route for ", destination)
		}
	}

	if handler == nil {
		handler = d.ohm.GetHandler(inTag) // Default outbound handler tag should be as same as the inbound tag
	}

	// If there is no outbound with tag as same as the inbound tag
	if handler == nil {
		handler = d.ohm.GetDefaultHandler()
	}

	if handler == nil {
		errors.LogInfo(ctx, "default outbound handler not exist")
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return
	}

	if accessMessage := log.AccessMessageFromContext(ctx); accessMessage != nil {
		if tag := handler.Tag(); tag != "" {
			if inTag == "" {
				accessMessage.Detour = tag
			} else if isPickRoute == 1 {
				accessMessage.Detour = inTag + " ==> " + tag
			} else if isPickRoute == 2 {
				accessMessage.Detour = inTag + " -> " + tag
			} else {
				accessMessage.Detour = inTag + " >> " + tag
			}
		}
		log.Record(accessMessage)
	}

	handler.Dispatch(ctx, link)
}
