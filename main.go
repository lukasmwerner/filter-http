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
	"nhooyr.io/websocket"
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

					if req.Header.Get("Upgrade") == "websocket" {
						log.Println("Switching protocols")
						handleWebsocket(r, req, selectedUpstream, route)
					}

					_, err := fetchUpstream(ctx, req.Method, req.Body, host, route, selectedUpstream, r)
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
				if errCount == 3 {
					log.Println("Too many failures. Guessing that upstreams are down.")
					r.WriteHeader(http.StatusServiceUnavailable)
					r.Write([]byte("{\"error\":\"Service Unavailable\"}"))
					return
				}
			}
		}
	}
}

func fetchUpstream(ctx context.Context, method string, body io.Reader, host string, route string, upstream string, w http.ResponseWriter) (int, error) {

	u, err := url.Parse(upstream)
	if err != nil {
		return 500, err
	}
	u.Path = path.Join(u.Path, route)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return 500, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 500, err
	}

	for k, v := range resp.Header {
		w.Header().Set(k, strings.Join(v, ", "))
	}

	if resp.StatusCode == http.StatusSwitchingProtocols {
		return http.StatusSwitchingProtocols, nil
	}

	w.WriteHeader(resp.StatusCode)

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return 500, err
	}

	return 200, nil

}

func handleWebsocket(r http.ResponseWriter, req *http.Request, selectedUpstream string, route string) {
	source, err := websocket.Accept(r, req, nil)
	if err != nil {
		log.Println("Error accept websocket:", err)
		return
	}

	u, err := url.Parse(selectedUpstream)
	if err != nil {
		log.Println("Error parse upstream:", err)
		return
	}
	u.Path = path.Join(u.Path, route)
	u.Scheme = "http"

	upstream, _, err := websocket.Dial(req.Context(), u.String(), nil)
	log.Println("Dialing:", u.String())
	if err != nil {
		log.Println("Error dialing websocket:", err)
		return
	}

	go func() {
		for {
			upstreamDatatype, upstreamData, err := upstream.Read(req.Context())
			if err != nil {
				log.Println("Error get upstream reader:", err)
				return
			}
			err = source.Write(req.Context(), upstreamDatatype, upstreamData)
			if err != nil {
				log.Println("Error write upstream:", err)
				return
			}
		}

	}()

	func() {
		for {
			sourceDatatype, sourceData, err := source.Read(req.Context())
			if err != nil {
				log.Println("Error get websocket reader:", err)
				return
			}
			err = upstream.Write(req.Context(), sourceDatatype, sourceData)
			if err != nil {
				log.Println("Error write downstream:", err)
				return
			}
		}
	}()
}
