// Command yami is a standalone build of the capture engine for desktop
// testing: it runs the proxy + API locally so you can point a desktop
// browser at 127.0.0.1:8899 and inspect http://127.0.0.1:8900/api/captures.
package main

import (
	"flag"
	"log"

	"yamiua/api"
	"yamiua/proxy"
)

func main() {
	proxyAddr := flag.String("proxy", "127.0.0.1:8899", "proxy listen address")
	apiAddr := flag.String("api", "127.0.0.1:8900", "API listen address")
	flag.Parse()

	p, err := proxy.New(*proxyAddr)
	if err != nil {
		log.Fatalf("proxy: %v", err)
	}
	a := api.NewAPI(p)

	go func() {
		log.Printf("[yami] API on %s", *apiAddr)
		if err := a.Listen(*apiAddr); err != nil {
			log.Fatal(err)
		}
	}()

	if err := p.Listen(); err != nil {
		log.Fatal(err)
	}
}
