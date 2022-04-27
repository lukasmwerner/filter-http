package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path"

	"github.com/gorilla/websocket"
)

var port = flag.String("port", "8084", "port to listen on")
var upgrader websocket.Upgrader

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
	http.HandleFunc("/api/404", func(r http.ResponseWriter, req *http.Request) {
		r.WriteHeader(http.StatusNotFound)
		r.Write([]byte("404 - Not Found"))
	})
	http.HandleFunc("/api/500", func(r http.ResponseWriter, req *http.Request) {
		r.WriteHeader(http.StatusInternalServerError)
		r.Write([]byte("500 - Internal Server Error"))
	})
	http.HandleFunc("/picture/", func(r http.ResponseWriter, req *http.Request) {
		r.Header().Set("Content-Type", "image/jpeg")
		filename := path.Base(req.URL.Path)
		f, err := os.Open("static/" + filename)
		if err != nil {
			r.WriteHeader(http.StatusNotFound)
			b, _ := json.Marshal(map[string]string{"error": "file not found"})
			r.Write(b)
			return
		}
		defer f.Close()
		_, err = io.Copy(r, f)
		if err != nil {
			r.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(map[string]string{"error": "internal server error"})
			r.Write(b)
			return
		}

	})
	http.HandleFunc("/api/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		log.Println("upgraded")
		if err != nil {
			log.Println(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(map[string]string{"error": "internal server error: " + err.Error()})
			w.Write(b)
			return
		}
		defer conn.Close()
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			conn.WriteMessage(msgType, msg)
		}
	})

	http.ListenAndServe(":"+*port, nil)
}
