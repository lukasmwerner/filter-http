package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ucarion/urlpath"
	"gopkg.in/yaml.v2"
)

var config Config

func main() {

	f, err := os.Open("config.yaml")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	err = yaml.NewDecoder(f).Decode(&config)
	if err != nil {
		panic(err)
	}

	fmt.Println("Starting server...")
	http.HandleFunc("/", webHandler)
	http.ListenAndServe(":8080", nil)
}

func webHandler(r http.ResponseWriter, req *http.Request) {
	host := req.Host
	route := req.URL.Path

	errCount := 0

	//log.Println("Request:", host, route)

	for i := range config.Servers {
		reqPath := urlpath.New(config.Servers[i].Route)

		if host == config.Servers[i].Host {
			if _, ok := reqPath.Match(route); ok {

				incrUp := func() {
					config.Servers[i].mu.Lock()
					config.Servers[i].upstreamIndex++
					config.Servers[i].mu.Unlock()
				}

				for errCount < 3 {
					ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
					defer cancel()

					config.Servers[i].mu.RLock()
					selectedUpstream := config.Servers[i].Upstreams[config.Servers[i].upstreamIndex%len(config.Servers[i].Upstreams)]
					config.Servers[i].mu.RUnlock()

					err := fetchUpstream(ctx, req.Method, req.Body, host, route, selectedUpstream, r)
					if err != nil {
						errCount++
						log.Println("Error:", err)
						incrUp()
						continue
					}
					incrUp()
					errCount = 4
					break
				}
			}
		}
	}
}

func fetchUpstream(ctx context.Context, method string, body io.Reader, host string, route string, upstream string, w http.ResponseWriter) error {

	u, err := url.Parse(upstream)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, route)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	for k, v := range resp.Header {
		w.Header().Set(k, strings.Join(v, ", "))
	}

	w.WriteHeader(resp.StatusCode)

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return err
	}

	return nil

}
