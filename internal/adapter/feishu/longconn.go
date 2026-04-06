package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

const (
	defaultReconnectInterval = 2 * time.Minute
	defaultPingInterval      = 2 * time.Minute
)

type GatewayState string

const (
	GatewayStateDisabled   GatewayState = "disabled"
	GatewayStateConnecting GatewayState = "connecting"
	GatewayStateConnected  GatewayState = "connected"
	GatewayStateDegraded   GatewayState = "degraded"
	GatewayStateAuthFailed GatewayState = "auth_failed"
	GatewayStateStopped    GatewayState = "stopped"
)

type VerifyResult struct {
	Connected    bool          `json:"connected"`
	ErrorCode    string        `json:"errorCode,omitempty"`
	ErrorMessage string        `json:"errorMessage,omitempty"`
	Duration     time.Duration `json:"duration"`
}

type gatewayRunnerError struct {
	code string
	err  error
}

func (e *gatewayRunnerError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	if e.code == "" {
		return e.err.Error()
	}
	return e.code + ": " + e.err.Error()
}

func (e *gatewayRunnerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *gatewayRunnerError) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

type gatewayWSRunner struct {
	config     LiveGatewayConfig
	dispatcher *dispatcher.EventDispatcher
	onState    func(GatewayState, error)
	httpClient *http.Client
	dialer     *websocket.Dialer
	writeMu    sync.Mutex
	combineMu  sync.Mutex
	combine    map[string][][]byte
	configMu   sync.RWMutex
	clientConf larkws.ClientConfig
}

func newGatewayWSRunner(config LiveGatewayConfig, dispatcher *dispatcher.EventDispatcher, onState func(GatewayState, error)) *gatewayWSRunner {
	config.GatewayID = normalizeGatewayID(config.GatewayID)
	if strings.TrimSpace(config.Domain) == "" {
		config.Domain = lark.FeishuBaseUrl
	}
	return &gatewayWSRunner{
		config:     config,
		dispatcher: dispatcher,
		onState:    onState,
		httpClient: http.DefaultClient,
		dialer:     websocket.DefaultDialer,
		combine:    map[string][][]byte{},
	}
}

func VerifyGatewayConnection(ctx context.Context, cfg LiveGatewayConfig) (VerifyResult, error) {
	started := time.Now()
	runner := newGatewayWSRunner(cfg, dispatcher.NewEventDispatcher("", ""), nil)
	connURL, clientConf, err := runner.fetchEndpoint(ctx)
	if err != nil {
		return verifyResultFromError(time.Since(started), err), err
	}
	conn, _, err := runner.dialer.DialContext(ctx, connURL, nil)
	if err != nil {
		return verifyResultFromError(time.Since(started), err), err
	}
	_ = conn.Close()
	runner.setClientConfig(clientConf)
	return VerifyResult{Connected: true, Duration: time.Since(started)}, nil
}

func verifyResultFromError(duration time.Duration, err error) VerifyResult {
	result := VerifyResult{
		Connected:    false,
		ErrorMessage: strings.TrimSpace(errString(err)),
		Duration:     duration,
	}
	var runnerErr *gatewayRunnerError
	if errors.As(err, &runnerErr) {
		result.ErrorCode = runnerErr.Code()
	}
	return result
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (r *gatewayWSRunner) Run(ctx context.Context) error {
	retryCount := 0
	for {
		if ctx.Err() != nil {
			r.emitState(GatewayStateStopped, nil)
			return nil
		}
		r.emitState(GatewayStateConnecting, nil)

		connURL, clientConf, err := r.fetchEndpoint(ctx)
		if err != nil {
			if ctx.Err() != nil {
				r.emitState(GatewayStateStopped, nil)
				return nil
			}
			if isGatewayAuthError(err) {
				r.emitState(GatewayStateAuthFailed, err)
				return err
			}
			r.emitState(GatewayStateDegraded, err)
			if !r.sleepBeforeRetry(ctx, retryCount) {
				r.emitState(GatewayStateStopped, nil)
				return nil
			}
			retryCount++
			continue
		}
		r.setClientConfig(clientConf)

		err = r.runSession(ctx, connURL)
		if ctx.Err() != nil {
			r.emitState(GatewayStateStopped, nil)
			return nil
		}
		if err == nil {
			r.emitState(GatewayStateStopped, nil)
			return nil
		}
		if isGatewayAuthError(err) {
			r.emitState(GatewayStateAuthFailed, err)
			return err
		}
		r.emitState(GatewayStateDegraded, err)
		if !r.sleepBeforeRetry(ctx, retryCount) {
			r.emitState(GatewayStateStopped, nil)
			return nil
		}
		retryCount++
	}
}

func (r *gatewayWSRunner) fetchEndpoint(ctx context.Context) (string, larkws.ClientConfig, error) {
	body := map[string]string{
		"AppID":     r.config.AppID,
		"AppSecret": r.config.AppSecret,
	}
	bs, err := json.Marshal(body)
	if err != nil {
		return "", larkws.ClientConfig{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.config.Domain, "/")+larkws.GenEndpointUri, bytes.NewBuffer(bs))
	if err != nil {
		return "", larkws.ClientConfig{}, err
	}
	req.Header.Add("locale", "zh")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", larkws.ClientConfig{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", larkws.ClientConfig{}, &gatewayRunnerError{
			code: classifyHTTPStatus(resp.StatusCode),
			err:  fmt.Errorf("endpoint request failed: status=%d", resp.StatusCode),
		}
	}

	var endpointResp larkws.EndpointResp
	if err := json.NewDecoder(resp.Body).Decode(&endpointResp); err != nil {
		return "", larkws.ClientConfig{}, err
	}
	switch endpointResp.Code {
	case larkws.OK:
	case larkws.AuthFailed, larkws.Forbidden:
		return "", larkws.ClientConfig{}, &gatewayRunnerError{
			code: "auth_failed",
			err:  fmt.Errorf("endpoint auth failed: code=%d msg=%s", endpointResp.Code, endpointResp.Msg),
		}
	default:
		return "", larkws.ClientConfig{}, &gatewayRunnerError{
			code: "connect_failed",
			err:  fmt.Errorf("endpoint rejected: code=%d msg=%s", endpointResp.Code, endpointResp.Msg),
		}
	}
	if endpointResp.Data == nil || strings.TrimSpace(endpointResp.Data.Url) == "" {
		return "", larkws.ClientConfig{}, &gatewayRunnerError{
			code: "connect_failed",
			err:  errors.New("endpoint URL is empty"),
		}
	}

	var clientConf larkws.ClientConfig
	if endpointResp.Data.ClientConfig != nil {
		clientConf = *endpointResp.Data.ClientConfig
	}
	return endpointResp.Data.Url, clientConf, nil
}

func (r *gatewayWSRunner) runSession(ctx context.Context, connURL string) error {
	conn, resp, err := r.dialer.DialContext(ctx, connURL, nil)
	if err != nil {
		if resp != nil {
			return r.handshakeError(resp)
		}
		return &gatewayRunnerError{code: "connect_failed", err: err}
	}
	defer conn.Close()

	r.emitState(GatewayStateConnected, nil)
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	go r.pingLoop(ctx, done, conn, serviceIDFromURL(connURL))

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return &gatewayRunnerError{code: "connect_failed", err: err}
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		if err := r.handleFrame(ctx, conn, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (r *gatewayWSRunner) pingLoop(ctx context.Context, done <-chan struct{}, conn *websocket.Conn, serviceID int32) {
	for {
		interval := r.currentPingInterval()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-done:
			timer.Stop()
			return
		case <-timer.C:
		}

		frame := larkws.NewPingFrame(serviceID)
		bs, err := frame.Marshal()
		if err != nil {
			continue
		}
		if err := r.writeMessage(conn, websocket.BinaryMessage, bs); err != nil {
			return
		}
	}
}

func (r *gatewayWSRunner) handleFrame(ctx context.Context, conn *websocket.Conn, msg []byte) error {
	var frame larkws.Frame
	if err := frame.Unmarshal(msg); err != nil {
		return err
	}

	switch larkws.FrameType(frame.Method) {
	case larkws.FrameTypeControl:
		return r.handleControlFrame(frame)
	case larkws.FrameTypeData:
		return r.handleDataFrame(ctx, conn, frame)
	default:
		return nil
	}
}

func (r *gatewayWSRunner) handleControlFrame(frame larkws.Frame) error {
	hs := larkws.Headers(frame.Headers)
	if larkws.MessageType(hs.GetString(larkws.HeaderType)) != larkws.MessageTypePong {
		return nil
	}
	if len(frame.Payload) == 0 {
		return nil
	}
	var conf larkws.ClientConfig
	if err := json.Unmarshal(frame.Payload, &conf); err != nil {
		return err
	}
	r.setClientConfig(conf)
	return nil
}

func (r *gatewayWSRunner) handleDataFrame(ctx context.Context, conn *websocket.Conn, frame larkws.Frame) error {
	hs := larkws.Headers(frame.Headers)
	sum := hs.GetInt(larkws.HeaderSum)
	seq := hs.GetInt(larkws.HeaderSeq)
	msgID := hs.GetString(larkws.HeaderMessageID)
	typeName := hs.GetString(larkws.HeaderType)

	payload := frame.Payload
	if sum > 1 {
		payload = r.combinePayload(msgID, sum, seq, payload)
		if payload == nil {
			return nil
		}
	}

	var (
		response any
		err      error
	)
	switch larkws.MessageType(typeName) {
	case larkws.MessageTypeEvent:
		response, err = r.dispatcher.Do(ctx, payload)
	case larkws.MessageTypeCard:
		return nil
	default:
		return nil
	}

	reply := larkws.NewResponseByCode(http.StatusOK)
	if err != nil {
		reply = larkws.NewResponseByCode(http.StatusInternalServerError)
	} else if response != nil {
		reply.Data, err = json.Marshal(response)
		if err != nil {
			reply = larkws.NewResponseByCode(http.StatusInternalServerError)
		}
	}

	replyPayload, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	frame.Payload = replyPayload
	frame.Headers = hs
	frameBytes, err := frame.Marshal()
	if err != nil {
		return err
	}
	return r.writeMessage(conn, websocket.BinaryMessage, frameBytes)
}

func (r *gatewayWSRunner) combinePayload(msgID string, sum, seq int, payload []byte) []byte {
	r.combineMu.Lock()
	defer r.combineMu.Unlock()
	buf := r.combine[msgID]
	if len(buf) == 0 {
		buf = make([][]byte, sum)
	}
	if seq < 0 || seq >= len(buf) {
		return nil
	}
	buf[seq] = payload
	for _, value := range buf {
		if len(value) == 0 {
			r.combine[msgID] = buf
			return nil
		}
	}
	delete(r.combine, msgID)
	size := 0
	for _, value := range buf {
		size += len(value)
	}
	combined := make([]byte, 0, size)
	for _, value := range buf {
		combined = append(combined, value...)
	}
	return combined
}

func (r *gatewayWSRunner) writeMessage(conn *websocket.Conn, messageType int, payload []byte) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return conn.WriteMessage(messageType, payload)
}

func (r *gatewayWSRunner) currentPingInterval() time.Duration {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	if r.clientConf.PingInterval > 0 {
		return time.Duration(r.clientConf.PingInterval) * time.Second
	}
	return defaultPingInterval
}

func (r *gatewayWSRunner) currentReconnectInterval() time.Duration {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	if r.clientConf.ReconnectInterval > 0 {
		return time.Duration(r.clientConf.ReconnectInterval) * time.Second
	}
	return defaultReconnectInterval
}

func (r *gatewayWSRunner) currentReconnectNonce() int {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	return r.clientConf.ReconnectNonce
}

func (r *gatewayWSRunner) setClientConfig(conf larkws.ClientConfig) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	r.clientConf = conf
}

func (r *gatewayWSRunner) sleepBeforeRetry(ctx context.Context, retryCount int) bool {
	wait := r.currentReconnectInterval()
	if retryCount == 0 {
		if nonce := r.currentReconnectNonce(); nonce > 0 {
			wait = time.Duration(nonce) * time.Second
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *gatewayWSRunner) emitState(state GatewayState, err error) {
	if r.onState != nil {
		r.onState(state, err)
	}
}

func (r *gatewayWSRunner) handshakeError(resp *http.Response) error {
	code, _ := strconv.Atoi(resp.Header.Get(larkws.HeaderHandshakeStatus))
	message := strings.TrimSpace(resp.Header.Get(larkws.HeaderHandshakeMsg))
	switch code {
	case larkws.AuthFailed, larkws.Forbidden:
		return &gatewayRunnerError{
			code: "auth_failed",
			err:  fmt.Errorf("handshake failed: code=%d msg=%s", code, message),
		}
	default:
		return &gatewayRunnerError{
			code: classifyHTTPStatus(resp.StatusCode),
			err:  fmt.Errorf("handshake failed: status=%d code=%d msg=%s", resp.StatusCode, code, message),
		}
	}
}

func classifyHTTPStatus(status int) string {
	switch status {
	case http.StatusForbidden, http.StatusUnauthorized:
		return "auth_failed"
	default:
		return "connect_failed"
	}
}

func isGatewayAuthError(err error) bool {
	var runnerErr *gatewayRunnerError
	return errors.As(err, &runnerErr) && runnerErr.Code() == "auth_failed"
}

func serviceIDFromURL(rawURL string) int32 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(parsed.Query().Get(larkws.ServiceID))
	return int32(value)
}
