package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertConstraints(t *testing.T) {
	dir := t.TempDir()
	caCrt, caKey := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	dns, ips := splitHosts([]string{"nas", "192.168.1.20", "nas.fritz.box"})
	ca, key, err := createCA(caCrt, caKey, dns, ips)
	if err != nil {
		t.Fatal(err)
	}
	crt, k := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	ok := [][]string{{"nas"}, {"nas.local", "10.1.2.3"}, {"x.nas.fritz.box"}, {"fd00::1"}, {"nas.home.arpa"}}
	for _, h := range ok {
		d, i := splitHosts(h)
		if err := issueCert(crt, k, ca, key, d, i); err != nil {
			t.Errorf("%v: %v", h, err)
		}
	}
	bad := [][]string{{"example.com"}, {"8.8.8.8"}, {"nas.local", "www.google.com"}, {"fritz.box"}}
	for _, h := range bad {
		d, i := splitHosts(h)
		if err := issueCert(crt, k, ca, key, d, i); err == nil {
			t.Errorf("%v: CA signed a name outside its constraints", h)
		}
	}
	if st, _ := os.Stat(caKey); st.Mode().Perm() != 0o600 {
		t.Errorf("ca.key mode %v", st.Mode().Perm())
	}
}

func TestCertStoreServesAndRenews(t *testing.T) {
	dir := t.TempDir()
	caCrt, caKey := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	crt, key := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	ca, signer, err := createCA(caCrt, caKey, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := issueCert(crt, key, ca, signer, []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1).To4()}); err != nil {
		t.Fatal(err)
	}
	cs, err := newCertStore(&Config{TLSCert: crt, TLSKey: key, TLSCA: caCrt})
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{TLSConfig: &tls.Config{GetCertificate: cs.GetCertificate}, Handler: http.NotFoundHandler()}
	go hs.ServeTLS(ln, "", "")
	defer hs.Close()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	resp, err := client.Get("https://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("client trusting only the CA: %v", err)
	}
	resp.Body.Close()

	// Pretend the certificate is about to expire: it gets renewed for the same names.
	old := cs.cert.Leaf
	cs.mu.Lock()
	cs.cert.Leaf = &x509.Certificate{NotAfter: time.Now().Add(time.Hour), DNSNames: old.DNSNames, IPAddresses: old.IPAddresses}
	cs.checked = time.Time{}
	cs.mu.Unlock()
	c, _ := cs.GetCertificate(nil)
	if c.Leaf.SerialNumber.Cmp(old.SerialNumber) == 0 || time.Until(c.Leaf.NotAfter) < leafValidity-24*time.Hour {
		t.Fatalf("certificate was not renewed")
	}
	if err := c.Leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
}
