package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type attempt struct {
	count int
	until time.Time
}
type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

func tokenHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (s *server) authed(r *http.Request) bool {
	c, err := r.Cookie("apptrail_session")
	if err != nil || len(c.Value) != 48 {
		return false
	}
	var n int
	return s.db.QueryRow("SELECT 1 FROM sessions WHERE token=? AND expires>?", tokenHash(c.Value), time.Now().Unix()).Scan(&n) == nil
}
func (s *server) ownerOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			fail(w, 401, "Sign in to continue")
			return
		}
		next(w, r)
	}
}
func (s *server) setSession(w http.ResponseWriter, r *http.Request) error {
	t := id()
	expiry := time.Now().Add(7 * 24 * time.Hour)
	if _, err := s.db.Exec("INSERT INTO sessions(token,expires) VALUES(?,?)", tokenHash(t), expiry.Unix()); err != nil {
		return err
	}
	_, _ = s.db.Exec("DELETE FROM sessions WHERE expires<?", time.Now().Unix())
	http.SetCookie(w, &http.Cookie{Name: "apptrail_session", Value: t, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(s.origin, "https://"), SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: 7 * 86400})
	return nil
}
func (s *server) session(w http.ResponseWriter, r *http.Request) {
	var username string
	err := s.db.QueryRow("SELECT username FROM owner WHERE id=1").Scan(&username)
	if err != nil && err != sql.ErrNoRows {
		dbError(w, err)
		return
	}
	a := s.authed(r)
	if !a {
		username = ""
	}
	reply(w, map[string]any{"authenticated": a, "setup_required": err == sql.ErrNoRows, "username": username})
}
func (s *server) allowLogin(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := time.Now()
	for k, a := range s.attempts {
		if now.After(a.until) {
			delete(s.attempts, k)
		}
	}
	a := s.attempts[ip]
	if a.count >= 10 {
		return false
	}
	if len(s.attempts) >= 4096 && a.count == 0 {
		return false
	}
	if a.count == 0 {
		a.until = now.Add(15 * time.Minute)
	}
	a.count++
	s.attempts[ip] = a
	return true
}
func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(r) {
		fail(w, 429, "Too many attempts. Try again in 15 minutes.")
		return
	}
	var c credentials
	if !body(w, r, &c) {
		return
	}
	if s.setupToken == "" || subtle.ConstantTimeCompare([]byte(c.Token), []byte(s.setupToken)) != 1 {
		fail(w, 403, "Invalid setup token")
		return
	}
	if !required(c.Username, 64) || len(c.Password) < 12 || len(c.Password) > 72 {
		fail(w, 400, "Choose a username and a password of 12–72 bytes")
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		dbError(w, err)
		return
	}
	res, err := s.db.Exec("INSERT OR IGNORE INTO owner(id,username,password) VALUES(1,?,?)", strings.TrimSpace(c.Username), string(h))
	if err != nil {
		dbError(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 409, "Owner already exists")
		return
	}
	if err = s.setSession(w, r); err != nil {
		dbError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(r) {
		fail(w, 429, "Too many attempts. Try again in 15 minutes.")
		return
	}
	var c credentials
	if !body(w, r, &c) {
		return
	}
	if len(c.Password) > 72 {
		fail(w, 401, "Incorrect username or password")
		return
	}
	var username, hash string
	err := s.db.QueryRow("SELECT username,password FROM owner WHERE id=1").Scan(&username, &hash)
	if err != nil && err != sql.ErrNoRows {
		dbError(w, err)
		return
	}
	if hash == "" {
		hash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5wLpxkSNIMSVNAgP82QgeJDsBX66YpG"
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(c.Password)) == nil
	if !valid || c.Username != username || err != nil {
		fail(w, 401, "Incorrect username or password")
		return
	}
	if err = s.setSession(w, r); err != nil {
		dbError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("apptrail_session"); err == nil {
		if _, err = s.db.Exec("DELETE FROM sessions WHERE token=?", tokenHash(c.Value)); err != nil {
			dbError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "apptrail_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	reply(w, map[string]bool{"ok": true})
}
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(r) {
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	var b struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !body(w, r, &b) {
		return
	}
	if len(b.Password) < 12 || len(b.Password) > 72 {
		fail(w, 400, "Password must contain 12–72 bytes")
		return
	}
	var hash string
	if err := s.db.QueryRow("SELECT password FROM owner WHERE id=1").Scan(&hash); err != nil {
		dbError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(b.Current)) != nil {
		fail(w, 403, "Current password is incorrect")
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
	if err != nil {
		dbError(w, err)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE owner SET password=? WHERE id=1", string(h)); err == nil {
		_, err = tx.Exec("DELETE FROM sessions")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if err = s.setSession(w, r); err != nil {
		dbError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
