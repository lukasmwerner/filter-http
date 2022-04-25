package main

import "sync"

type Config struct {
	Servers []Server `json:"servers" yaml:"servers"`
}
type Server struct {
	Host          string   `json:"host" yaml:"host"`
	Route         string   `json:"route" yaml:"route"`
	Upstreams     []string `json:"upstreams" yaml:"upstreams"`
	mu            sync.RWMutex
	upstreamIndex int
}
