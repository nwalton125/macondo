// Command endgameui runs a local web server for interactively exploring
// endgame and pre-endgame (peg) analyses on top of macondo's solvers.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/domino14/macondo/config"
	"github.com/domino14/macondo/endgameui"
)

//go:embed static
var staticFiles embed.FS

func main() {
	addr := flag.String("addr", "localhost:8000", "address to listen on")
	dataPath := flag.String("data-path", "./data", "path to macondo data directory (lexica, letter distributions)")
	open := flag.Bool("open", true, "automatically open a browser window")
	logLevel := flag.String("log-level", "warn", "log verbosity: trace, debug, info, warn, error")
	flag.Parse()

	// The solvers log a debug/trace line per internal node visited; at the
	// default zerolog level that's enough I/O to turn a few-second solve
	// into minutes. Keep it quiet unless the user asks for it.
	lvl, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid -log-level")
	}
	zerolog.SetGlobalLevel(lvl)

	cfg := config.DefaultConfig()
	cfg.Set(config.ConfigDataPath, *dataPath)

	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal().Err(err).Msg("bad embedded static root")
	}

	srv := endgameui.NewServer(cfg, staticRoot)

	url := "http://" + *addr
	fmt.Printf("endgame UI listening at %s\n", url)
	if *open {
		go openBrowser(url)
	}

	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
