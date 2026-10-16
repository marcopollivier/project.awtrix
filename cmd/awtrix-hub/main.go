// Command awtrix-hub feeds an AWTRIX NG clock (Ulanzi TC001) with social counters
// and live football, and ships a browser simulator of the clock.
//
//	awtrix-hub sim                     # simulator on http://localhost:7070
//	awtrix-hub run [--sim]             # poll sources and update the targets
//	awtrix-hub run --mock              # fake data for every source, in the simulator
//	awtrix-hub preview                 # fetch every source once and exit
//	awtrix-hub set linkedin 2890       # manual counters (also updates the clock's script)
//	awtrix-hub deploy                  # install scripts/*.ax on the clock (runs without the hub)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/api"
	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/deploy"
	"github.com/marcopollivier/project.awtrix/internal/mock"
	"github.com/marcopollivier/project.awtrix/internal/scheduler"
	"github.com/marcopollivier/project.awtrix/internal/sim"
	"github.com/marcopollivier/project.awtrix/internal/sources"
	"github.com/marcopollivier/project.awtrix/internal/sources/football"
	"github.com/marcopollivier/project.awtrix/internal/sources/github"
	"github.com/marcopollivier/project.awtrix/internal/sources/instagram"
	"github.com/marcopollivier/project.awtrix/internal/sources/linkedin"
	"github.com/marcopollivier/project.awtrix/internal/sources/youtube"
	"github.com/marcopollivier/project.awtrix/internal/state"
)

var version = "dev"

const usage = `uso: awtrix-hub <comando> [flags]

comandos:
  sim       inicia o simulador web do relógio
  run       consulta as fontes e atualiza o relógio/simulador
  preview   consulta cada fonte uma vez, envia e sai
  set       grava um contador manual (ex.: set linkedin 2890) e atualiza o relógio
  icons     envia os ícones da config para o relógio (o AWTRIX não baixa sozinho)
  deploy    instala os scripts no relógio (AWTRIX NG), que passa a funcionar sem o hub
  version   mostra a versão
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "sim":
		err = cmdSim(ctx, args)
	case "run":
		err = cmdRun(ctx, args, false)
	case "preview":
		err = cmdRun(ctx, args, true)
	case "set":
		err = cmdSet(args)
	case "icons":
		err = cmdIcons(ctx, args)
	case "deploy":
		err = cmdDeploy(ctx, args)
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// loadConfig reads the file if it exists; without one, defaults are fine for the simulator.
func loadConfig(path string, log *slog.Logger) (*config.Config, error) {
	cfg, err := config.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		log.Warn("config não encontrada, usando padrões", "path", path)
		cfg = config.Default()
		if err := cfg.Finish(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	return cfg, err
}

func cmdSim(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("sim", flag.ExitOnError)
	cfgPath := fl.String("config", "config.yaml", "arquivo de configuração")
	addr := fl.String("addr", "", "endereço (padrão: sim.addr da config)")
	fl.Parse(args)
	log := newLogger(false)
	cfg, err := loadConfig(*cfgPath, log)
	if err != nil {
		return err
	}
	if *addr == "" {
		*addr = cfg.Sim.Addr
	}
	return serveSim(ctx, cfg, *addr, log, nil)
}

// serveSim blocks until ctx ends. ready (optional) is closed once listening.
func serveSim(ctx context.Context, cfg *config.Config, addr string, log *slog.Logger, ready chan<- struct{}) error {
	s := sim.New(log.With("component", "sim"), filepath.Join(cfg.DataDir, "icons"), cfg.IconsDir)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	stop := make(chan struct{})
	go s.Run(stop)
	go func() {
		<-ctx.Done()
		close(stop)
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("simulador no ar", "url", "http://"+ln.Addr().String())
	if ready != nil {
		close(ready)
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return ctx.Err()
}

func cmdRun(ctx context.Context, args []string, once bool) error {
	name := "run"
	if once {
		name = "preview"
	}
	fl := flag.NewFlagSet(name, flag.ExitOnError)
	cfgPath := fl.String("config", "config.yaml", "arquivo de configuração")
	withSim := fl.Bool("sim", false, "sobe o simulador junto e envia para ele também")
	fixture := fl.String("football-fixture", "", "diretório com um jogo gravado (ex.: internal/sources/football/testdata/match)")
	only := fl.String("only", "", "roda só esta fonte (github, youtube, instagram, linkedin, football)")
	debug := fl.Bool("debug", false, "logs detalhados")
	useMock := fl.Bool("mock", false, "dados de mentira para todas as fontes (implica --sim; não toca no estado real)")
	fl.Parse(args)
	if *useMock {
		*withSim = true
		if *fixture == "" {
			*fixture = defaultFixture
		}
	}
	log := newLogger(*debug)

	cfg, err := loadConfig(*cfgPath, log)
	if err != nil {
		return err
	}
	if *withSim {
		ready := make(chan struct{})
		go func() {
			if err := serveSim(ctx, cfg, cfg.Sim.Addr, log, ready); err != nil && !errors.Is(err, context.Canceled) {
				log.Error("simulador", "err", err)
			}
		}()
		select {
		case <-ready:
		case <-time.After(2 * time.Second):
			return fmt.Errorf("simulador não subiu em %s", cfg.Sim.Addr)
		}
		simURL := "http://" + cfg.Sim.Addr
		if *useMock {
			cfg.Targets = []string{simURL} // fake data never reaches the real clock
		} else if !slices.Contains(cfg.Targets, simURL) {
			cfg.Targets = append(cfg.Targets, simURL)
		}
	}

	var mockURL string
	if *useMock {
		if mockURL, err = setupMock(ctx, cfg); err != nil {
			return err
		}
		log.Info("modo mock: contadores sobem a cada 5s e batem metas em menos de 1 minuto")
	}

	st, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return err
	}
	srcs, err := buildSources(cfg, st, *fixture, *only, mockURL, log)
	if err != nil {
		return err
	}
	if len(srcs) == 0 {
		return errors.New("nenhuma fonte habilitada")
	}
	client := awtrix.NewClient(cfg.Targets, log.With("component", "awtrix")).WithAuth(cfg.Device.User, cfg.Device.Pass)
	if err := syncIcons(ctx, cfg, client, log, false); err != nil {
		log.Warn("ícones não sincronizados", "err", err)
	}
	if cfg.API.Addr != "" && !once {
		go func() {
			if err := serveAPI(ctx, cfg.API.Addr, st, log); err != nil && !errors.Is(err, context.Canceled) {
				log.Error("endpoint de métricas", "err", err)
			}
		}()
	}
	sched := scheduler.New(cfg, client, st, log, srcs...)
	log.Info("enviando para", "targets", cfg.Targets)
	if once {
		sched.RunOnce(ctx)
		return nil
	}
	sched.Run(ctx)
	return ctx.Err()
}

// serveAPI serves the metrics endpoint the clock's scripts read until ctx ends.
func serveAPI(ctx context.Context, addr string, st *state.Store, log *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: api.Handler(st, time.Now), ReadHeaderTimeout: 5 * time.Second}
	context.AfterFunc(ctx, func() { srv.Close() })
	log.Info("endpoint de métricas no ar", "url", "http://"+ln.Addr().String()+"/api/v1/metrics")
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return ctx.Err()
}

const defaultFixture = "internal/sources/football/testdata/match"

// setupMock points the config at fake APIs and a throwaway data dir.
// It must run after the simulator started (the icon cache stays in the real dir).
func setupMock(ctx context.Context, cfg *config.Config) (string, error) {
	srv := mock.Server()
	context.AfterFunc(ctx, srv.Close)

	tmp, err := os.MkdirTemp("", "awtrix-hub-mock")
	if err != nil {
		return "", err
	}
	cfg.DataDir = tmp
	if err := state.SetManual(manualPath(cfg), linkedin.Key, 2890, time.Now()); err != nil {
		return "", err
	}
	sc := &cfg.Sources
	sc.GitHub.Enabled, sc.GitHub.Interval = true, 5*time.Second
	sc.YouTube.Enabled, sc.YouTube.Interval, sc.YouTube.APIKey = true, 5*time.Second, "mock"
	sc.Instagram.Enabled, sc.Instagram.Interval = true, 5*time.Second
	sc.Instagram.Token = "mock"
	sc.LinkedIn.Enabled = true
	sc.Football.Enabled = true
	return srv.URL, nil
}

func buildSources(cfg *config.Config, st *state.Store, fixture, only, mockURL string, log *slog.Logger) ([]sources.Source, error) {
	sc := cfg.Sources
	enabled := func(name string, on bool) bool {
		if only != "" {
			return name == only
		}
		return on
	}
	var srcs []sources.Source
	if enabled("github", sc.GitHub.Enabled) {
		g := github.New(sc.GitHub, cfg.Icon)
		if mockURL != "" {
			g.BaseURL = mockURL + "/github"
		}
		srcs = append(srcs, g)
	}
	if enabled("youtube", sc.YouTube.Enabled) {
		if sc.YouTube.APIKey == "" {
			log.Warn("youtube habilitado mas YOUTUBE_API_KEY não definida; pulando")
		} else {
			y := youtube.New(sc.YouTube, cfg.Icon)
			if mockURL != "" {
				y.BaseURL = mockURL + "/youtube"
			}
			srcs = append(srcs, y)
		}
	}
	if enabled("instagram", sc.Instagram.Enabled) {
		if sc.Instagram.Token == "" {
			log.Warn("instagram habilitado mas INSTAGRAM_TOKEN não definido; pulando")
		} else {
			ig := instagram.New(sc.Instagram, cfg.Icon, filepath.Join(cfg.DataDir, "instagram_token.json"))
			if mockURL != "" {
				ig.BaseURL = mockURL + "/instagram"
			}
			srcs = append(srcs, ig)
		}
	}
	if enabled("linkedin", sc.LinkedIn.Enabled) {
		srcs = append(srcs, linkedin.New(sc.LinkedIn, cfg.Icon, manualPath(cfg)))
	}
	if enabled("football", sc.Football.Enabled) || fixture != "" {
		f := football.New(sc.Football, cfg.Icon, st, cfg.Location)
		if fixture != "" {
			srv, err := football.FixtureServer(fixture, time.Now().Add(2*time.Minute))
			if err != nil {
				return nil, err
			}
			// The replay uses its own state so it never pollutes the real one.
			tmp, err := os.MkdirTemp("", "awtrix-hub-fixture")
			if err != nil {
				return nil, err
			}
			fst, err := state.Open(filepath.Join(tmp, "state.json"))
			if err != nil {
				return nil, err
			}
			f = football.New(sc.Football, cfg.Icon, fst, cfg.Location)
			f.BaseURL, f.Fast = srv.URL, true
			log.Info("futebol em modo replay", "fixture", fixture)
		}
		srcs = append(srcs, f)
	}
	return srcs, nil
}

func cmdSet(args []string) error {
	fl := flag.NewFlagSet("set", flag.ExitOnError)
	cfgPath := fl.String("config", "config.yaml", "arquivo de configuração")
	fl.Parse(args)
	if fl.NArg() != 2 || fl.Arg(0) != linkedin.Key {
		return errors.New("uso: awtrix-hub set linkedin <seguidores>")
	}
	v, err := strconv.ParseInt(fl.Arg(1), 10, 64)
	if err != nil || v < 0 {
		return fmt.Errorf("valor inválido: %q", fl.Arg(1))
	}
	cfg, err := loadConfig(*cfgPath, newLogger(false))
	if err != nil {
		return err
	}
	if err := state.SetManual(manualPath(cfg), fl.Arg(0), v, time.Now()); err != nil {
		return err
	}
	fmt.Printf("%s = %d (o run, se estiver rodando, envia em até 1 minuto)\n", fl.Arg(0), v)
	if cfg.Device.URL == "" {
		return nil
	}
	// The clock's linkedin script reads its value from @config.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := awtrix.NewClient(nil, newLogger(false)).WithAuth(cfg.Device.User, cfg.Device.Pass)
	if err := client.PatchAppConfig(ctx, cfg.Device.URL, "linkedin", map[string]any{"followers": v}); err != nil {
		return fmt.Errorf("relógio (%s) não atualizado: %w", cfg.Device.URL, err)
	}
	fmt.Println("relógio atualizado:", cfg.Device.URL)
	return nil
}

func cmdDeploy(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("deploy", flag.ExitOnError)
	cfgPath := fl.String("config", "config.yaml", "arquivo de configuração")
	target := fl.String("target", "", "relógio (padrão: device.url da config)")
	only := fl.String("only", "", "instala só estes scripts, separados por vírgula (ex.: counter,github)")
	noIcons := fl.Bool("no-icons", false, "não envia os ícones")
	fl.Parse(args)
	log := newLogger(false)
	cfg, err := loadConfig(*cfgPath, log)
	if err != nil {
		return err
	}
	if *target == "" {
		*target = cfg.Device.URL
	}
	if *target == "" {
		return errors.New("defina device.url na config ou use --target http://<ip-do-relógio>")
	}
	scripts, err := deploy.Load(cfg.Scripts)
	if err != nil {
		return err
	}
	client := awtrix.NewClient([]string{*target}, log).WithAuth(cfg.Device.User, cfg.Device.Pass)
	if !*noIcons {
		if err := syncIcons(ctx, cfg, client, log, false); err != nil {
			log.Warn("ícones não sincronizados", "err", err)
		}
	}
	var followers *int64
	if m, err := state.ReadManual(manualPath(cfg)); err == nil {
		if v, ok := m[linkedin.Key]; ok {
			followers = &v.Value
		}
	}
	var names []string
	if *only != "" {
		names = strings.Split(*only, ",")
	}
	if err := deploy.Deploy(ctx, client, *target, scripts, deploy.Settings(cfg, followers), names, log); err != nil {
		return err
	}
	fmt.Println("pronto: o relógio busca os dados sozinho; o hub é opcional")
	return nil
}

func cmdIcons(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("icons", flag.ExitOnError)
	cfgPath := fl.String("config", "config.yaml", "arquivo de configuração")
	force := fl.Bool("force", false, "reenvia mesmo os ícones que já estão no relógio")
	fl.Parse(args)
	log := newLogger(false)
	cfg, err := loadConfig(*cfgPath, log)
	if err != nil {
		return err
	}
	return syncIcons(ctx, cfg, awtrix.NewClient(cfg.Targets, log), log, *force)
}

// syncIcons uploads the configured icons (LaMetric ids or project icons from
// icons_dir) to every target that has a file manager (the real clock); the
// simulator loads icons by itself.
func syncIcons(ctx context.Context, cfg *config.Config, client *awtrix.Client, log *slog.Logger, force bool) error {
	ids := slices.Sorted(maps.Values(cfg.Icons))
	ids = slices.Compact(ids)
	lametric := sim.NewIcons(filepath.Join(cfg.DataDir, "icons"), cfg.IconsDir)
	var errs []error
	for _, target := range client.Targets() {
		have, ok, err := client.Icons(ctx, target)
		if err != nil {
			log.Warn("target fora do ar, ícones não verificados", "target", target, "err", err)
			continue
		}
		if !ok {
			continue // simulator
		}
		sent := 0
		for _, id := range ids {
			if !force && (slices.Contains(have, id+".gif") || slices.Contains(have, id+".jpg")) {
				continue
			}
			raw, err := lametric.Raw(ctx, id)
			if err == nil {
				var data []byte
				if data, err = awtrix.IconGIF(raw); err == nil {
					err = client.UploadIcon(ctx, target, id, data)
				}
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s ícone %s: %w", target, id, err))
				continue
			}
			sent++
		}
		log.Info("ícones no relógio", "target", target, "enviados", sent, "total", len(ids))
	}
	return errors.Join(errs...)
}

func manualPath(cfg *config.Config) string { return filepath.Join(cfg.DataDir, "manual.json") }
