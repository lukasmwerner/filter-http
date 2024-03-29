package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ucarion/urlpath"
	"gopkg.in/yaml.v2"
	"nhooyr.io/websocket"
)

var config Config

var domainMap map[string]*Server

var accessLog log.Logger
var errorLog log.Logger

func main() {

	f, err := os.Open("config.yaml")
	if err != nil {
		f, err := os.Open("config.yml")
		if err != nil {
			f, err := os.Open("config.json")
			if err != nil {
				panic(err)
			}
			dec := json.NewDecoder(f)
			err = dec.Decode(&config)
			if err != nil {
				panic(err)
			}

		}
		err = yaml.NewDecoder(f).Decode(&config)
		if err != nil {
			panic(err)
		}
		panic(err)
	}
	defer f.Close()

	err = yaml.NewDecoder(f).Decode(&config)
	if err != nil {
		panic(err)
	}

	domainMap = make(map[string]*Server)
	for i := range config.Servers {
		configHost, err := url.Parse("http://" + config.Servers[i].Host)
		if err != nil {
			log.Println("Error parse host:", err)
			continue
		}
		// sort routes by longest to shortest
		sort.Slice(config.Servers[i].Routes, func(a, b int) bool {
			return len(config.Servers[i].Routes[a].Route) > len(config.Servers[i].Routes[b].Route)
		})
		domainMap[configHost.Hostname()] = &config.Servers[i]
	}
	accessLog.SetOutput(os.Stdout)
	errorLog.SetOutput(os.Stderr)

	fmt.Print("registered domains and routes in matching order:\n\n")
	for k := range domainMap {
		for i := range domainMap[k].Routes {
			fmt.Printf("%s: %s -> %v\n", k, domainMap[k].Routes[i].Route, domainMap[k].Routes[i].Upstreams)
		}
	}

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", configHandler)
		http.ListenAndServe(":9000", mux)
	}()

	fmt.Println()
	fmt.Println("Starting server...")
	http.HandleFunc("/", webHandler)
	http.ListenAndServe(":80", nil)
}

func webHandler(r http.ResponseWriter, req *http.Request) {
	host := req.Host

	errCount := 0

	log.Println("Request:", req.Method, host, req.URL.Path)

	u, err := url.Parse("http://" + host)
	if err != nil {
		log.Println("Error parse host:", err)
		return
	}

	server, ok := domainMap[u.Hostname()]
	if !ok {
		errorLog.Println("Error:", "No server for host:", u.Hostname())
		http.Error(r, "No server for host: "+u.Hostname(), http.StatusBadRequest)
		return
	}

	for i := range server.Routes {

		reqPath := urlpath.New(server.Routes[i].Route)

		if _, ok := reqPath.Match(req.URL.Path); ok {

			incrUp := func() {
				server.Routes[i].mu.Lock()
				server.Routes[i].upstreamIndex++
				server.Routes[i].mu.Unlock()
			}

			for errCount < 3 {
				ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
				defer cancel()

				server.Routes[i].mu.RLock()
				selectedUpstream := server.Routes[i].Upstreams[server.Routes[i].upstreamIndex%len(server.Routes[i].Upstreams)]
				server.Routes[i].mu.RUnlock()

				if req.Header.Get("Upgrade") == "websocket" {
					handleWebsocket(r, req, selectedUpstream, req.URL.String())
				}

				_, err := fetchUpstream(ctx, req.Method, req.Body, host, req.URL.String(), selectedUpstream, r)
				if err != nil {
					errCount++
					errorLog.Println("Error:", err)
					incrUp()
					continue
				}
				incrUp()
				errCount = 4
				break
			}
			if errCount == 3 {
				errorLog.Println("Too many failures. Guessing that upstreams are down.")
				http.Error(r, fmt.Errorf("error service unavailable: guessing that upstreams are down: too many failures").Error(), http.StatusServiceUnavailable)
				return
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
		if k == "Server" {
			w.Header().Set("Server", "filter-http")
			continue
		}
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
		errorLog.Println("Error accept websocket:", err)
		return
	}

	u, err := url.Parse(selectedUpstream)
	if err != nil {
		errorLog.Println("Error parse upstream:", err)
		return
	}
	u.Path = path.Join(u.Path, route)
	u.Scheme = "http"

	upstream, _, err := websocket.Dial(req.Context(), u.String(), nil)
	log.Println("Dialing:", u.String())
	if err != nil {
		errorLog.Println("Error dialing websocket:", err)
		return
	}

	go func() {
		for {
			upstreamDatatype, upstreamData, err := upstream.Read(req.Context())
			if err != nil {
				errorLog.Println("Error get upstream reader:", err)
				return
			}
			err = source.Write(req.Context(), upstreamDatatype, upstreamData)
			if err != nil {
				errorLog.Println("Error write upstream:", err)
				return
			}
		}

	}()

	func() {
		for {
			sourceDatatype, sourceData, err := source.Read(req.Context())
			if err != nil {
				errorLog.Println("Error get websocket reader:", err)
				return
			}
			err = upstream.Write(req.Context(), sourceDatatype, sourceData)
			if err != nil {
				errorLog.Println("Error write downstream:", err)
				return
			}
		}
	}()
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	accessLog.Printf("%s %s %s", r.Method, r.Host, r.URL.Path)
	if r.Method == "GET" {

		if r.URL.Query().Has("format") && r.URL.Query().Get("format") == "yaml" {
			w.Header().Set("Content-Type", "application/yaml")
			data, _ := yaml.Marshal(config)
			w.Write(data)
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(config)
		}
		return
	}
	if r.Method == "POST" {

		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			json.NewDecoder(r.Body).Decode(&config)
			data, _ := json.Marshal(config)
			w.Write(data)
			return
		}

		matcher := urlpath.New("/config/:server/route/:route/upstream/:upstream")
		match, ok := matcher.Match(r.URL.Path)
		if !ok {
			matcher := urlpath.New("/config/:server/route/:route/upstream")
			match, ok := matcher.Match(r.URL.Path)
			if !ok {
				matcher := urlpath.New("/config/:server/route/:route")
				match, ok := matcher.Match(r.URL.Path)
				if !ok {
					matcher := urlpath.New("/config/:server")
					match, ok := matcher.Match(r.URL.Path)
					if !ok {
						http.Error(w, "Invalid path", http.StatusBadRequest)
						return
					}
					serverindex, err := strconv.Atoi(match.Params["server"])
					if err != nil {
						http.Error(w, "Invalid server", http.StatusBadRequest)
						return
					}
					var server Server
					err = json.NewDecoder(r.Body).Decode(&server)
					if err != nil {
						http.Error(w, "Invalid server", http.StatusBadRequest)
						return
					}

					config.Servers[serverindex] = server
					sort.Slice(config.Servers[serverindex].Routes, func(a, b int) bool {
						return len(config.Servers[serverindex].Routes[a].Route) > len(config.Servers[serverindex].Routes[b].Route)
					})
					domainMap[config.Servers[serverindex].Host] = &config.Servers[serverindex]
					return

				}
				server, err := strconv.Atoi(match.Params["server"])
				if err != nil {
					http.Error(w, fmt.Sprintf("Invalid server: %s", match.Params["server"]), http.StatusBadRequest)
					return
				}
				routeindex, err := strconv.Atoi(match.Params["route"])
				if err != nil {
					http.Error(w, fmt.Sprintf("Invalid route: %s", match.Params["route"]), http.StatusBadRequest)
					return
				}
				var route Route
				err = json.NewDecoder(r.Body).Decode(&route)
				if err != nil {
					http.Error(w, "Invalid upstreams", http.StatusBadRequest)
					return
				}
				config.Servers[server].Routes[routeindex] = route
				sort.Slice(config.Servers[server].Routes, func(a, b int) bool {
					return len(config.Servers[server].Routes[a].Route) > len(config.Servers[server].Routes[b].Route)
				})
				domainMap[config.Servers[server].Host] = &config.Servers[server]
				return
			}
			server, err := strconv.Atoi(match.Params["server"])
			if err != nil {
				http.Error(w, "Invalid server", http.StatusBadRequest)
				return
			}
			route, err := strconv.Atoi(match.Params["route"])
			if err != nil {
				http.Error(w, "Invalid route", http.StatusBadRequest)
				return
			}
			var upstreams []string
			err = json.NewDecoder(r.Body).Decode(&upstreams)
			if err != nil {
				http.Error(w, "Invalid upstreams", http.StatusBadRequest)
				return
			}
			config.Servers[server].Routes[route].Upstreams = upstreams
			domainMap[config.Servers[server].Host] = &config.Servers[server]
			return
		}
		server, err := strconv.Atoi(match.Params["server"])
		if err != nil {
			http.Error(w, "Invalid server", http.StatusBadRequest)
			return
		}
		route, err := strconv.Atoi(match.Params["route"])
		if err != nil {
			http.Error(w, "Invalid route", http.StatusBadRequest)
			return
		}
		upstream, err := strconv.Atoi(match.Params["upstream"])
		if err != nil {
			http.Error(w, "Invalid upstream", http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(r.Body)
		config.Servers[server].Routes[route].Upstreams[upstream] = string(data)
		domainMap[config.Servers[server].Host] = &config.Servers[server]
		return
	}
}
