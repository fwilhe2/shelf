package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

// User is someone who can log in.
type User struct {
	Name         string `json:"name"`
	PasswordHash string `json:"password_hash"`
}

// Share is a directory on disk that is made available on the network.
type Share struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	ReadOnly bool     `json:"read_only,omitempty"`
	Users    []string `json:"users,omitempty"` // empty means: every user
}

// AllowedFor reports whether user may access the share.
func (s Share) AllowedFor(user string) bool {
	return len(s.Users) == 0 || slices.Contains(s.Users, user)
}

// Config is the whole mynas configuration, stored as a single JSON file.
type Config struct {
	Listen  string  `json:"listen"`
	TLSCert string  `json:"tls_cert,omitempty"`
	TLSKey  string  `json:"tls_key,omitempty"`
	TLSCA   string  `json:"tls_ca,omitempty"` // offered for download at /ca.crt
	Users   []User  `json:"users"`
	Shares  []Share `json:"shares"`
}

var (
	userNameRe  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}$`)
	shareNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_. -]{0,63}$`)
)

func validUserName(n string) bool { return userNameRe.MatchString(n) }

func validShareName(n string) bool {
	return shareNameRe.MatchString(n) && strings.TrimRight(n, " .") == n
}

// Validate checks the config for mistakes.
func (c *Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen address must not be empty")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tls_cert and tls_key must be set together")
	}
	if c.TLSCA != "" && c.TLSCert == "" {
		return errors.New("tls_ca needs tls_cert and tls_key")
	}
	users := map[string]bool{}
	for _, u := range c.Users {
		if !validUserName(u.Name) {
			return fmt.Errorf("invalid user name %q", u.Name)
		}
		if users[u.Name] {
			return fmt.Errorf("duplicate user %q", u.Name)
		}
		if u.PasswordHash == "" {
			return fmt.Errorf("user %q has no password", u.Name)
		}
		users[u.Name] = true
	}
	shares := map[string]bool{}
	for _, s := range c.Shares {
		if !validShareName(s.Name) {
			return fmt.Errorf("invalid share name %q (letters, digits, space, '.', '_', '-')", s.Name)
		}
		if shares[s.Name] {
			return fmt.Errorf("duplicate share %q", s.Name)
		}
		if !filepath.IsAbs(s.Path) {
			return fmt.Errorf("share %q: path must be absolute, got %q", s.Name, s.Path)
		}
		shares[s.Name] = true
	}
	return nil
}

// User returns the user with the given name, or nil.
func (c *Config) User(name string) *User {
	for i := range c.Users {
		if c.Users[i].Name == name {
			return &c.Users[i]
		}
	}
	return nil
}

// ShareIndex returns the index of the named share, or -1.
func (c *Config) ShareIndex(name string) int {
	return slices.IndexFunc(c.Shares, func(s Share) bool { return s.Name == name })
}

// SharesFor returns the shares a user may access.
func (c *Config) SharesFor(user string) []Share {
	var out []Share
	for _, s := range c.Shares {
		if s.AllowedFor(user) {
			out = append(out, s)
		}
	}
	return out
}

// LoadConfig reads and validates the config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config %s not found (run 'mynas init' first, or pass -c)", path)
		}
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Save writes the config atomically, keeping owner and mode of an existing file.
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mynas-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	mode := os.FileMode(0o600) // contains password hashes
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			_ = tmp.Chown(int(sys.Uid), int(sys.Gid)) // keep the service user able to read it
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
