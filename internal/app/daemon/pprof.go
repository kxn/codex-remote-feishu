package daemon

import (
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

const (
	defaultPprofListenHost = "127.0.0.1"
	defaultPprofListenPort = 17501
)

func defaultPprofBindAddr() string {
	return pprofBindAddrFromConfig(config.PprofSettings{Enabled: true})
}

func pprofBindAddrForDebugSettings(settings config.DebugSettings) string {
	if settings.Pprof == nil {
		return ""
	}
	return pprofBindAddrFromConfig(*settings.Pprof)
}

func pprofBindAddrFromConfig(settings config.PprofSettings) string {
	if !settings.Enabled {
		return ""
	}
	host := strings.TrimSpace(settings.ListenHost)
	if host == "" {
		host = defaultPprofListenHost
	}
	port := settings.ListenPort
	if port <= 0 {
		port = defaultPprofListenPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (a *App) ConfigurePprof(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		a.pprofServer = nil
		return
	}
	a.pprofServer = &http.Server{
		Addr:    addr,
		Handler: newPprofMux(),
	}
}

func (a *App) PprofURL() string {
	a.listenMu.Lock()
	defer a.listenMu.Unlock()

	if a.pprofServer == nil {
		return ""
	}
	if a.pprofListener != nil {
		return pprofURLForAddr(a.pprofListener.Addr().String())
	}
	return pprofURLForAddr(a.pprofServer.Addr)
}

func (a *App) bindPprofListenerLocked() {
	if a.pprofServer == nil || a.pprofListener != nil {
		return
	}
	listener, err := net.Listen("tcp", a.pprofServer.Addr)
	if err != nil {
		log.Printf("pprof disabled: bind %s failed: %v", a.pprofServer.Addr, err)
		a.pprofServer = nil
		return
	}
	a.pprofListener = listener
}

func newPprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

func pprofURLForAddr(addr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return ""
	}
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		host = defaultPprofListenHost
	}
	if port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port) + "/debug/pprof/"
}
