package wrapperhost

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// fakeManager stands in for wrapper-manager run natively: it installs the
// payload from -payload-url, records its sha256 and working directory, serves
// /status on -port and records a SIGTERM before it exits.
func fakeManager(args []string) {
	var port, url string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-port":
			port = args[i+1]
		case "-payload-url":
			url = args[i+1]
		}
	}
	resp, err := http.Get(url)
	if err != nil {
		os.Exit(6)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	sum := sha256.Sum256(body)
	os.WriteFile("payload.sha256", []byte(hex.EncodeToString(sum[:])), 0o600)
	os.WriteFile("args", []byte(strings.Join(args, "\n")), 0o600)
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	go func() {
		<-sigs
		os.WriteFile("terminated", nil, 0o600)
		os.Exit(0)
	}()
	fmt.Println("wrapperManager running")
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"ready":true,"clientCount":0,"regions":[],"instances":[]}}`)
	}))
}
