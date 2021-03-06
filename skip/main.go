// Copyright 2021 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gorilla/handlers"
	"github.com/netsec-ethz/scion-apps/pkg/shttp"
)

var (
	mungedScionAddr = regexp.MustCompile(`^(\d+)-([_\dA-Fa-f]+)-(.*)$`)
)

const (
	mungedScionAddrIAIndex   = 1
	mungedScionAddrASIndex   = 2
	mungedScionAddrHostIndex = 3
)

func main() {
	transport := shttp.NewRoundTripper(&tls.Config{InsecureSkipVerify: true}, nil)
	defer transport.Close()
	proxy := &proxyHandler{
		transport: transport,
	}

	server := &http.Server{
		Addr:    "localhost:8888",
		Handler: handlers.LoggingHandler(os.Stdout, proxy),
	}
	log.Fatal(server.ListenAndServe())
}

type proxyHandler struct {
	transport http.RoundTripper
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	hostMunged := req.Host
	host := demunge(req.Host)
	req.Host = host
	req.URL.Scheme = "https"
	req.URL.Host = host
	// Only accept plain text so we can munge the host name in the body without decompressing (lazy)
	req.Header.Del("Accept-Encoding")

	resp, err := h.transport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyAndReplaceHeader(w.Header(), resp.Header, host, hostMunged)
	w.WriteHeader(resp.StatusCode)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		fmt.Println("replacing")
		copyAndReplace(w, resp.Body, host, hostMunged)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
}

func copyAndReplaceHeader(dst, src http.Header, host, hostMunged string) {
	for k, vv := range src {
		for _, v := range vv {
			vMunged := replaceMunged([]byte(v), host, hostMunged)
			dst.Add(k, string(vMunged))
		}
	}
}

func copyAndReplace(w io.Writer, body io.Reader, host, hostMunged string) {
	// ReadAll, not the most elegant solution...
	b, _ := ioutil.ReadAll(body)
	b = replaceMunged(b, host, hostMunged)
	_, _ = w.Write(b)
}

// replaceMunged replaces http://<host> or https://<host> with http://<hostMunged>, so
// for example it replaces https://www.scionlab.org with http://www.scionlab.org.scion
// This replacement is applied to both headers and html body so that most links and redirects
// should work.
func replaceMunged(s []byte, host, hostMunged string) []byte {
	// compile and compile again, not super elegant either...
	reOriginal := regexp.MustCompile(`http(s)?://` + regexp.QuoteMeta(host))
	return reOriginal.ReplaceAll(s, []byte("http://"+hostMunged))
}

// demunge reverts the host name to a proper SCION address, from the format
// that had been entered in the browser.
func demunge(host string) string {
	parts := mungedScionAddr.FindStringSubmatch(host)
	if parts != nil {
		// directly apply mangling as in appnet.MangleSCIONAddr
		return fmt.Sprintf("[%s-%s,%s]",
			parts[mungedScionAddrIAIndex],
			strings.ReplaceAll(parts[mungedScionAddrASIndex], "_", ":"),
			parts[mungedScionAddrHostIndex],
		)
	} else {
		return strings.TrimSuffix(host, ".scion")
	}
}
