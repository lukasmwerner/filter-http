package main

import (
	"flag"
	"net/http"
)

var port = flag.String("port", "8084", "port to listen on")

func main() {
	flag.Parse()

	http.HandleFunc("/", func(r http.ResponseWriter, req *http.Request) {
		r.Write([]byte("Hello, World! from :" + *port + "\n"))
		r.Write([]byte("Request: " + req.Host + req.URL.Path + "\n"))
	})
	http.HandleFunc("/api/json", func(r http.ResponseWriter, req *http.Request) {
		r.Header().Set("Content-Type", "application/json")
		r.Write([]byte("{\"message\": \"Hello, World!\"}"))
	})
	http.ListenAndServe(":"+*port, nil)
}
