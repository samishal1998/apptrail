package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

//go:embed web/dist/*
var frontend embed.FS

var version = "dev"

type server struct {
	db         *sql.DB
	setupToken string
	origin     string
	scans      sync.Mutex
	loginMu    sync.Mutex
	attempts   map[string]attempt
}

func id() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }

func openStore(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS owner (id INTEGER PRIMARY KEY CHECK(id=1), username TEXT NOT NULL, password TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (token TEXT PRIMARY KEY, expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS applications (id TEXT PRIMARY KEY, created INTEGER NOT NULL, overrides TEXT NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS identities (key TEXT PRIMARY KEY, app_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS observations (provider TEXT NOT NULL, source TEXT NOT NULL, app_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE, payload TEXT NOT NULL, present INTEGER NOT NULL DEFAULT 1, seen INTEGER NOT NULL, PRIMARY KEY(provider, source));
CREATE TABLE IF NOT EXISTS providers (id TEXT PRIMARY KEY, name TEXT NOT NULL, endpoint TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, scanned INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS dashboards (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE, public INTEGER NOT NULL DEFAULT 0, items TEXT NOT NULL DEFAULT '[]');
CREATE TABLE IF NOT EXISTS settings (id INTEGER PRIMARY KEY CHECK(id=1), private_probes INTEGER NOT NULL DEFAULT 0);
INSERT OR IGNORE INTO settings(id) VALUES(1);
INSERT OR IGNORE INTO dashboards(id,name,slug) VALUES('home','Overview','home');`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func main() {
	showVersion := flag.Bool("version", false, "Print the Apptrail version and exit")
	addr := flag.String("addr", "0.0.0.0:8080", "HTTP listen address")
	data := flag.String("data", "data", "Persistent data directory")
	reset := flag.Bool("reset-password", false, "Reset owner password from APPTRAIL_NEW_PASSWORD and revoke sessions")
	flag.Parse()
	if *showVersion {
		fmt.Printf("Apptrail %s\n", version)
		return
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		log.Fatal(err)
	}
	db, err := openStore(filepath.Join(*data, "apptrail.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if *reset {
		p := os.Getenv("APPTRAIL_NEW_PASSWORD")
		if len(p) < 12 || len(p) > 72 {
			log.Fatal("APPTRAIL_NEW_PASSWORD must contain 12–72 bytes")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			log.Fatal(err)
		}
		res, err := tx.Exec("UPDATE owner SET password=? WHERE id=1", string(hash))
		if err != nil {
			log.Fatal(err)
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			log.Fatal("Owner has not been set up")
		}
		if _, err = tx.Exec("DELETE FROM sessions"); err != nil {
			log.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			log.Fatal(err)
		}
		log.Print("Owner password changed; all sessions revoked")
		return
	}
	s := &server{db: db, origin: strings.TrimRight(os.Getenv("APPTRAIL_ORIGIN"), "/"), attempts: make(map[string]attempt)}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM owner").Scan(&count); err != nil {
		log.Fatal(err)
	}
	if count == 0 {
		p := filepath.Join(*data, "setup-token")
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			b = []byte(id())
			err = os.WriteFile(p, b, 0600)
		}
		if err != nil {
			log.Fatal(err)
		}
		s.setupToken = strings.TrimSpace(string(b))
		log.Printf("First-run setup token: %s", s.setupToken)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	scanDone := make(chan struct{})
	go func() { defer close(scanDone); s.scheduler(ctx) }()
	h := &http.Server{Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.Shutdown(c)
	}()
	log.Printf("Apptrail listening on http://%s", *addr)
	if err := h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	stop()
	<-shutdownDone
	<-scanDone
}

func (s *server) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/session", s.session)
	m.HandleFunc("POST /api/setup", s.setup)
	m.HandleFunc("POST /api/login", s.login)
	m.HandleFunc("POST /api/logout", s.logout)
	m.HandleFunc("GET /api/public/{slug}", s.publicDashboard)
	m.HandleFunc("GET /api/icons/{id}", s.icon)
	m.HandleFunc("GET /api/apps", s.ownerOnly(s.listApps))
	m.HandleFunc("POST /api/apps", s.ownerOnly(s.createApp))
	m.HandleFunc("PATCH /api/apps/{id}", s.ownerOnly(s.updateApp))
	m.HandleFunc("DELETE /api/apps/{id}", s.ownerOnly(s.deleteApp))
	m.HandleFunc("POST /api/apps/{id}/refresh", s.ownerOnly(s.refreshApp))
	m.HandleFunc("GET /api/providers", s.ownerOnly(s.listProviders))
	m.HandleFunc("POST /api/providers", s.ownerOnly(s.saveProvider))
	m.HandleFunc("PATCH /api/providers/{id}", s.ownerOnly(s.saveProvider))
	m.HandleFunc("DELETE /api/providers/{id}", s.ownerOnly(s.deleteProvider))
	m.HandleFunc("POST /api/providers/{id}/scan", s.ownerOnly(s.scanProviderHTTP))
	m.HandleFunc("GET /api/dashboards", s.ownerOnly(s.listDashboards))
	m.HandleFunc("POST /api/dashboards", s.ownerOnly(s.saveDashboard))
	m.HandleFunc("PUT /api/dashboards/{id}", s.ownerOnly(s.saveDashboard))
	m.HandleFunc("DELETE /api/dashboards/{id}", s.ownerOnly(s.deleteDashboard))
	m.HandleFunc("GET /api/settings", s.ownerOnly(s.getSettings))
	m.HandleFunc("PUT /api/settings", s.ownerOnly(s.putSettings))
	m.HandleFunc("POST /api/account", s.ownerOnly(s.changePassword))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "Not found") })
	assets, _ := fs.Sub(frontend, "web/dist")
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "Method not allowed")
			return
		}
		if r.URL.Path != "/" {
			if f, err := assets.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				f.Close()
				http.FileServer(http.FS(assets)).ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			fail(w, 500, "Frontend build missing")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			allowed := s.origin
			if allowed == "" {
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				allowed = scheme + "://" + r.Host
			}
			if (origin != "" && origin != allowed) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Cross-origin request rejected")
				return
			}
		}
		m.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func body(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "Expected application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, 400, "Invalid request: "+err.Error())
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		fail(w, 400, "Expected one JSON object")
		return false
	}
	return true
}
func dbError(w http.ResponseWriter, err error) {
	log.Print(err)
	fail(w, 500, "Could not save or read data")
}
func required(v string, max int) bool { return strings.TrimSpace(v) != "" && len(v) <= max }
func notFound(w http.ResponseWriter)  { fail(w, 404, "Not found") }
func changed(w http.ResponseWriter, res sql.Result, err error) bool {
	if err != nil {
		dbError(w, err)
		return false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		notFound(w)
		return false
	}
	return true
}
