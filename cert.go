package main

// HTTPS without a public certificate authority: `mynas cert` creates a small
// private CA once and uses it to sign the server certificate. Each device
// trusts the CA once (download it from /ca.crt), and from then on the
// connection is trusted, even when the server certificate is renewed or
// re-issued for new host names.
//
// The CA is name-constrained: it can only sign certificates for private IP
// addresses, .local/.lan/.home.arpa/.internal names and the host names given
// when it was created. If ca.key leaks, it can't be used to impersonate
// other websites to devices that trust it.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	caValidity   = 20 * 365 * 24 * time.Hour
	leafValidity = 825 * 24 * time.Hour // Apple rejects longer-lived server certificates
	renewBefore  = 30 * 24 * time.Hour
)

var (
	homeDNSSuffixes = []string{"localhost", "local", "lan", "home.arpa", "internal"}
	privateNets     = []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
		"169.254.0.0/16", "100.64.0.0/10", // link-local, CGNAT (Tailscale)
		"::1/128", "fc00::/7", "fe80::/10",
	}
)

func cmdCert(args []string) error {
	fs, cfgPath := newFlags("cert")
	hostsFlag := fs.String("hosts", "", "comma separated names and IP addresses clients use to reach the NAS (default: this machine's name and addresses)")
	newCA := fs.Bool("new-ca", false, "create a new CA (devices have to trust the new one)")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return err
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		return err
	}

	var hosts []string
	for _, h := range strings.Split(*hostsFlag, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		if _, err := os.Stat("/run/.containerenv"); err == nil {
			return errors.New("inside a container, pass -hosts with the names and IP addresses clients use, e.g. -hosts nas.local,192.168.1.20")
		}
		hosts = defaultHosts()
	}
	dnsNames, ips := splitHosts(hosts)

	dir, err := filepath.Abs(filepath.Dir(*cfgPath))
	if err != nil {
		return err
	}
	caCrt, caKey := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	crt, key := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")

	ca, signer, err := loadCA(caCrt, caKey)
	if *newCA || errors.Is(err, os.ErrNotExist) {
		if ca, signer, err = createCA(caCrt, caKey, dnsNames, ips); err != nil {
			return err
		}
		fmt.Printf("Created a new CA: %s\n", caCrt)
	} else if err != nil {
		return err
	}

	if err := issueCert(crt, key, ca, signer, dnsNames, ips); err != nil {
		return err
	}
	cfg.TLSCert, cfg.TLSKey, cfg.TLSCA = crt, key, caCrt
	if err := cfg.Save(*cfgPath); err != nil {
		return err
	}
	fmt.Printf(`Created a certificate for %s (mynas renews it automatically).
HTTPS is turned on. Restart mynas to use it.

On every device, open https://<your-nas>%s/ca.crt (accept the warning once),
and trust the downloaded certificate. See the README for how this works on
each system. Keep %s secret.
`, strings.Join(hosts, ", "), portSuffix(cfg.Listen), caKey)
	return nil
}

func portSuffix(listen string) string {
	if _, port, err := net.SplitHostPort(listen); err == nil && port != "443" {
		return ":" + port
	}
	return ""
}

func defaultHosts() []string {
	hosts := []string{"localhost", "127.0.0.1"}
	if h, err := os.Hostname(); err == nil && h != "" && h != "localhost" {
		hosts = append(hosts, h)
		if !strings.Contains(h, ".") {
			hosts = append(hosts, h+".local") // mDNS / Avahi
		}
	}
	ifaddrs, _ := net.InterfaceAddrs()
	for _, a := range ifaddrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
			hosts = append(hosts, ipn.IP.String())
		}
	}
	return hosts
}

func splitHosts(hosts []string) (dnsNames []string, ips []net.IP) {
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			}
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, strings.ToLower(h))
		}
	}
	return dnsNames, ips
}

func newSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func createCA(crtPath, keyPath string, dnsNames []string, ips []net.IP) (*x509.Certificate, crypto.Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "mynas CA " + host, Organization: []string{"mynas"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,

		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(homeDNSSuffixes), dnsNames...)))),
	}
	for _, n := range privateNets {
		_, ipn, _ := net.ParseCIDR(n)
		tmpl.PermittedIPRanges = append(tmpl.PermittedIPRanges, ipn)
	}
	for _, ip := range ips {
		if !slices.ContainsFunc(tmpl.PermittedIPRanges, func(n *net.IPNet) bool { return n.Contains(ip) }) {
			tmpl.PermittedIPRanges = append(tmpl.PermittedIPRanges, &net.IPNet{IP: ip, Mask: net.CIDRMask(len(ip)*8, len(ip)*8)})
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	if err := writeKeyPair(crtPath, keyPath, der, key); err != nil {
		return nil, nil, err
	}
	ca, err := x509.ParseCertificate(der)
	return ca, key, err
}

func loadCA(crtPath, keyPath string) (*x509.Certificate, crypto.Signer, error) {
	pair, err := tls.LoadX509KeyPair(crtPath, keyPath)
	if err != nil {
		return nil, nil, err
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, nil, fmt.Errorf("%s: unsupported key type", keyPath)
	}
	return pair.Leaf, signer, nil
}

// issueCert signs a server certificate and checks that clients will accept it.
func issueCert(crtPath, keyPath string, ca *x509.Certificate, caKey crypto.Signer, dnsNames []string, ips []net.IP) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := newSerial()
	if err != nil {
		return err
	}
	cn := "mynas"
	if len(dnsNames) > 0 {
		cn = dnsNames[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		return fmt.Errorf("the existing CA can't sign for these names (%v); run again with -new-ca", err)
	}
	return writeKeyPair(crtPath, keyPath, der, key)
}

func writeKeyPair(crtPath, keyPath string, der []byte, key *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return writeFileAtomic(crtPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mynas-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ---------------------------------------------------------------- serving

// certStore serves the configured certificate. It picks up a changed file
// without a restart and renews a certificate issued by `mynas cert` shortly
// before it expires (when ca.key is next to the CA certificate).
type certStore struct {
	crt, key, caCrt string

	mu      sync.Mutex
	cert    *tls.Certificate
	mod     time.Time
	checked time.Time
}

func newCertStore(cfg *Config) (*certStore, error) {
	cs := &certStore{crt: cfg.TLSCert, key: cfg.TLSKey, caCrt: cfg.TLSCA}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if err := cs.refresh(); err != nil {
		return nil, err
	}
	return cs, nil
}

func (cs *certStore) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if time.Since(cs.checked) > time.Minute {
		if err := cs.refresh(); err != nil {
			log.Printf("TLS certificate: %v (keeping the old one)", err)
		}
	}
	return cs.cert, nil
}

// refresh reloads the certificate if the file changed and renews it if needed. cs.mu must be held.
func (cs *certStore) refresh() error {
	cs.checked = time.Now()
	st, err := os.Stat(cs.crt)
	if err != nil {
		return err
	}
	if cs.cert == nil || !st.ModTime().Equal(cs.mod) {
		pair, err := tls.LoadX509KeyPair(cs.crt, cs.key)
		if err != nil {
			return err
		}
		cs.cert, cs.mod = &pair, st.ModTime()
	}
	leaf := cs.cert.Leaf
	if time.Until(leaf.NotAfter) > renewBefore || cs.caCrt == "" {
		return nil
	}
	ca, signer, err := loadCA(cs.caCrt, strings.TrimSuffix(cs.caCrt, ".crt")+".key")
	if errors.Is(err, os.ErrNotExist) {
		return nil // not ours to renew
	} else if err != nil {
		return err
	}
	if err := issueCert(cs.crt, cs.key, ca, signer, leaf.DNSNames, leaf.IPAddresses); err != nil {
		return fmt.Errorf("renewing: %w", err)
	}
	log.Printf("renewed the TLS certificate")
	cs.cert = nil
	return cs.refresh()
}
