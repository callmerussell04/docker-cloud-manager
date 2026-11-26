// This executable runs inside an untrusted Dockerfile RUN, not on the host.
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "network" {
		for _, arg := range os.Args[2:] {
			parts := strings.SplitN(arg, "=", 2)
			conn, err := net.DialTimeout("tcp", parts[1], 2*time.Second)
			if conn != nil {
				conn.Close()
			}
			if (err == nil) != (parts[0] == "allow") {
				fmt.Fprintln(os.Stderr, "unexpected network access:", arg, err)
				os.Exit(1)
			}
		}
		return
	}

	if err := probe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func probe() error {
	authority, own, other, digest, upload := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	data, err := os.ReadFile("/kaniko/.docker/config.json")
	if err != nil {
		return fmt.Errorf("read job auth config")
	}
	var config struct {
		Auths map[string]struct{ Auth string }
	}
	if err = json.Unmarshal(data, &config); err != nil {
		return err
	}
	ca, err := os.ReadFile("/kaniko/ssl/certs/dcm-registry.crt")
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 10 * time.Second}
	scopes := []string{"repository:" + own + ":pull,push,delete", "repository:" + other + ":pull,push,delete", "registry:catalog:*", "repository:*:*"}
	q := url.Values{"service": []string{"dcm-registry"}, "scope": scopes}
	req, _ := http.NewRequest("GET", "https://"+authority+"/token?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Basic "+config.Auths[authority].Auth)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("exchange job credential: %w", err)
	}
	var tokens struct{ Token string }
	err = json.NewDecoder(resp.Body).Decode(&tokens)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || tokens.Token == "" {
		return fmt.Errorf("job exchange failed: %d", resp.StatusCode)
	}
	for _, test := range []struct{ method, path string }{
		{"GET", "/v2/_catalog"}, {"GET", "/v2/" + other + "/manifests/latest"},
		{"HEAD", "/v2/" + other + "/manifests/" + digest}, {"GET", "/v2/" + other + "/tags/list"},
		{"POST", "/v2/" + other + "/blobs/uploads/"}, {"PUT", "/v2/" + other + "/manifests/latest"},
		{"DELETE", "/v2/" + other + "/manifests/" + digest}, {"DELETE", "/v2/" + own + "/manifests/" + digest},
		{"GET", "/v2/" + other + "/blobs/" + digest}, {"DELETE", "/v2/" + other + "/blobs/" + digest},
		{"PATCH", upload}, {"PUT", upload + "&digest=" + digest},
	} {
		req, _ = http.NewRequest(test.method, "https://"+authority+test.path, bytes.NewReader([]byte("hostile payload")))
		req.Header.Set("Authorization", "Bearer "+tokens.Token)
		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("probe request failed: %w", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 401 && resp.StatusCode != 403 {
			return fmt.Errorf("%s was not denied (%d)", test.method+" "+strings.Split(test.path, "?")[0], resp.StatusCode)
		}
	}
	// A cross-repository mount may fall back to a new upload, never disclose B's blob.
	req, _ = http.NewRequest("POST", "https://"+authority+"/v2/"+own+"/blobs/uploads/?from="+url.QueryEscape(other)+"&mount="+url.QueryEscape(digest), nil)
	req.Header.Set("Authorization", "Bearer "+tokens.Token)
	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 202 && resp.StatusCode != 401 && resp.StatusCode != 403 {
		return fmt.Errorf("unauthorized cross-owner mount: %d", resp.StatusCode)
	}
	fmt.Println("cross-owner probes denied")
	// The first Kaniko token must be unusable at push time, even with the
	// pinned Registry verifier's 60-second leeway. Exercise real client renewal.
	time.Sleep(125 * time.Second)
	return nil
}
