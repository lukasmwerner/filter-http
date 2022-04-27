package main

import "sync"

type Config struct {
	Servers []Server `json:"servers" yaml:"servers"`
}
type Server struct {
	Host   string  `json:"host" yaml:"host"`
	Routes []Route `json:"routes" yaml:"routes"`
}
type Route struct {
	Route         string   `json:"route" yaml:"route"`
	Upstreams     []string `json:"upstream" yaml:"upstreams"`
	mu            sync.RWMutex
	upstreamIndex int
}
