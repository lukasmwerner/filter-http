# Filter-HTTP

a basic reverse proxy, load balancing proxy that supports websockets and has dynamic dns loading to allow for use in containers

Here is a sample config
```yaml
servers:
- host: localhost
  routes:
  - route: /*
    upstreams:
    - http://localhost:8080
    - http://localhost:8081
  - route: /api/*
    upstreams:
    - http://localhost:8080
    - http://localhost:8081
```
