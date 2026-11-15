// Command mynas is a small, simple NAS for the home network: a web UI to
// browse, upload and download files, and WebDAV so shares can be mounted as
// network drives on Linux, macOS and Windows.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const usageText = `mynas - simple home NAS

Usage:
  mynas init  [-data DIR] [-listen ADDR]     create a config with one share and a first user
  mynas serve                                run the server

  mynas user list
  mynas user add NAME                        add a user (asks for a password)
  mynas user passwd NAME                     change a password
  mynas user del NAME

  mynas share list
  mynas share add NAME DIR [-ro] [-users a,b]  share a directory (default: all users, read-write)
  mynas share del NAME                       stop sharing (files are kept)

Every command accepts -c FILE (default: $MYNAS_CONFIG or /etc/mynas/config.json).
Changes to users and shares are picked up by a running server automatically.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "init":
		err = cmdInit(args)
	case "serve":
		err = cmdServe(args)
	case "user":
		err = cmdUser(args)
	case "share":
		err = cmdShare(args)
	case "help", "-h", "-help", "--help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	if p := os.Getenv("MYNAS_CONFIG"); p != "" {
		return p
	}
	return "/etc/mynas/config.json"
}

func newFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("mynas "+name, flag.ContinueOnError)
	cfg := fs.String("c", defaultConfigPath(), "config file")
	return fs, cfg
}

// parseArgs parses flags that may appear before, between or after positional arguments.
func parseArgs(fs *flag.FlagSet, args []string, wantPositional int) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		pos, args = append(pos, args[0]), args[1:]
	}
	if len(pos) != wantPositional {
		return nil, fmt.Errorf("expected %d argument(s), got %d (see 'mynas help')", wantPositional, len(pos))
	}
	return pos, nil
}

// ---------------------------------------------------------------- prompts

var stdin = bufio.NewReader(os.Stdin)

func isTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func readLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readSecret(prompt string) (string, error) {
	if !isTerminal() { // allow: echo secret | mynas user add bob
		return readLine("")
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func promptNewPasswordHash(user string) (string, error) {
	p1, err := readSecret(fmt.Sprintf("New password for %s: ", user))
	if err != nil {
		return "", err
	}
	if p1 == "" {
		return "", errors.New("password must not be empty")
	}
	if isTerminal() {
		p2, err := readSecret("Repeat password: ")
		if err != nil {
			return "", err
		}
		if p1 != p2 {
			return "", errors.New("passwords do not match")
		}
	}
	h, err := bcrypt.GenerateFromPassword([]byte(p1), bcrypt.DefaultCost)
	return string(h), err
}

// ---------------------------------------------------------------- init

func cmdInit(args []string) error {
	fs, cfgPath := newFlags("init")
	dataDir := fs.String("data", "/srv/mynas", "directory to store files in")
	listen := fs.String("listen", ":8080", "address to listen on")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return err
	}
	if _, err := os.Stat(*cfgPath); err == nil {
		return fmt.Errorf("%s already exists", *cfgPath)
	}
	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	def := os.Getenv("SUDO_USER")
	if def == "" {
		def = os.Getenv("USER")
	}
	name, err := readLine(fmt.Sprintf("Name of the first user [%s]: ", def))
	if err != nil {
		return err
	}
	if name = strings.TrimSpace(name); name == "" {
		name = def
	}
	if !validUserName(name) {
		return fmt.Errorf("invalid user name %q", name)
	}
	hash, err := promptNewPasswordHash(name)
	if err != nil {
		return err
	}

	cfg := &Config{
		Listen: *listen,
		Users:  []User{{Name: name, PasswordHash: hash}},
		Shares: []Share{{Name: "files", Path: dir}},
	}
	if err := cfg.Save(*cfgPath); err != nil {
		return err
	}
	fmt.Printf("Created %s with share \"files\" -> %s\nStart the server with: mynas serve -c %s\n", *cfgPath, dir, *cfgPath)
	return nil
}

// ---------------------------------------------------------------- serve

func cmdServe(args []string) error {
	fs, cfgPath := newFlags("serve")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return err
	}
	srv, err := NewServer(*cfgPath)
	if err != nil {
		return err
	}
	cfg := srv.config()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.watchConfig(ctx)

	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No read/write timeouts: uploads and downloads of big files can take long.
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(shutdownCtx)
	}()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	scheme := "http"
	if cfg.TLSCert != "" {
		scheme = "https"
	}
	log.Printf("mynas is running. Open one of these in a browser:")
	for _, u := range urls(scheme, ln.Addr().(*net.TCPAddr)) {
		log.Printf("  %s/        (WebDAV: %s/dav/)", u, u)
	}

	if cfg.TLSCert != "" {
		err = hs.ServeTLS(ln, cfg.TLSCert, cfg.TLSKey)
	} else {
		err = hs.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// urls lists the addresses under which the server is reachable.
func urls(scheme string, addr *net.TCPAddr) []string {
	port := addr.Port
	var hosts []string
	if !addr.IP.IsUnspecified() {
		hosts = append(hosts, addr.IP.String())
	} else {
		if h, err := os.Hostname(); err == nil {
			hosts = append(hosts, h)
		}
		ifaddrs, _ := net.InterfaceAddrs()
		for _, a := range ifaddrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				hosts = append(hosts, ipn.IP.String())
			}
		}
		if len(hosts) == 0 {
			hosts = append(hosts, "localhost")
		}
	}
	var out []string
	for _, h := range hosts {
		out = append(out, fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(h, fmt.Sprint(port))))
	}
	return out
}

// ---------------------------------------------------------------- users

func cmdUser(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mynas user list|add|passwd|del [NAME]")
	}
	sub, args := args[0], args[1:]
	fs, cfgPath := newFlags("user " + sub)
	want := 1
	if sub == "list" {
		want = 0
	}
	pos, err := parseArgs(fs, args, want)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		return err
	}

	switch sub {
	case "list":
		for _, u := range cfg.Users {
			fmt.Println(u.Name)
		}
		return nil
	case "add":
		name := pos[0]
		if !validUserName(name) {
			return fmt.Errorf("invalid user name %q", name)
		}
		if cfg.User(name) != nil {
			return fmt.Errorf("user %q already exists", name)
		}
		hash, err := promptNewPasswordHash(name)
		if err != nil {
			return err
		}
		cfg.Users = append(cfg.Users, User{Name: name, PasswordHash: hash})
		fmt.Printf("Added user %q.\n", name)
	case "passwd":
		u := cfg.User(pos[0])
		if u == nil {
			return fmt.Errorf("no such user %q", pos[0])
		}
		if u.PasswordHash, err = promptNewPasswordHash(u.Name); err != nil {
			return err
		}
		fmt.Printf("Password for %q changed.\n", u.Name)
	case "del":
		n := len(cfg.Users)
		cfg.Users = slices.DeleteFunc(cfg.Users, func(u User) bool { return u.Name == pos[0] })
		if len(cfg.Users) == n {
			return fmt.Errorf("no such user %q", pos[0])
		}
		fmt.Printf("Deleted user %q.\n", pos[0])
	default:
		return fmt.Errorf("unknown command 'user %s'", sub)
	}
	return cfg.Save(*cfgPath)
}

// ---------------------------------------------------------------- shares

func cmdShare(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mynas share list|add|del ...")
	}
	sub, args := args[0], args[1:]
	fs, cfgPath := newFlags("share " + sub)
	var ro *bool
	var users *string
	want := map[string]int{"list": 0, "add": 2, "del": 1}[sub]
	if sub == "add" {
		ro = fs.Bool("ro", false, "read-only share")
		users = fs.String("users", "", "comma separated users allowed to access the share (default: all)")
	}
	pos, err := parseArgs(fs, args, want)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		return err
	}

	switch sub {
	case "list":
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tPATH\tMODE\tUSERS")
		for _, s := range cfg.Shares {
			mode, who := "read-write", "all"
			if s.ReadOnly {
				mode = "read-only"
			}
			if len(s.Users) > 0 {
				who = strings.Join(s.Users, ",")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Path, mode, who)
		}
		return tw.Flush()
	case "add":
		name := pos[0]
		if !validShareName(name) {
			return fmt.Errorf("invalid share name %q", name)
		}
		if cfg.ShareIndex(name) >= 0 {
			return fmt.Errorf("share %q already exists", name)
		}
		dir, err := filepath.Abs(pos[1])
		if err != nil {
			return err
		}
		if fi, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			fmt.Printf("Created directory %s\n", dir)
		} else if err != nil {
			return err
		} else if !fi.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		sh := Share{Name: name, Path: dir, ReadOnly: *ro}
		for _, u := range strings.Split(*users, ",") {
			if u = strings.TrimSpace(u); u == "" {
				continue
			}
			if cfg.User(u) == nil {
				return fmt.Errorf("no such user %q", u)
			}
			sh.Users = append(sh.Users, u)
		}
		cfg.Shares = append(cfg.Shares, sh)
		fmt.Printf("Sharing %s as %q.\n", dir, name)
	case "del":
		i := cfg.ShareIndex(pos[0])
		if i < 0 {
			return fmt.Errorf("no such share %q", pos[0])
		}
		fmt.Printf("Stopped sharing %q. The files in %s were not touched.\n", pos[0], cfg.Shares[i].Path)
		cfg.Shares = append(cfg.Shares[:i], cfg.Shares[i+1:]...)
	default:
		return fmt.Errorf("unknown command 'share %s'", sub)
	}
	return cfg.Save(*cfgPath)
}
