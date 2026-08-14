// Command fake-hkex serves deterministic DTOP/RP006 ZIPs for the HKEX CLI
// acceptance test. It is test infrastructure only; production uses HKEX URLs.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	dtopPath = regexp.MustCompile(`^/DTOP_O_(\d{8})\.zip$`)
	rpPath   = regexp.MustCompile(`^/RP006_(\d{6})\.zip$`)
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18087", "listen address")
	class := flag.String("class", "TST", "option class")
	flag.Parse()
	cls := strings.ToUpper(strings.TrimSpace(*class))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		if match := dtopPath.FindStringSubmatch(r.URL.Path); match != nil {
			date, err := time.Parse("20060102", match[1])
			if err != nil || weekend(date) {
				http.NotFound(w, r)
				return
			}
			serveZip(w, match[1]+"_1_dtop_o_seoch_opt_dtl_all.raw", dtop(cls, date))
			return
		}
		if match := rpPath.FindStringSubmatch(r.URL.Path); match != nil {
			date, err := time.Parse("060102", match[1])
			if err != nil || weekend(date) {
				http.NotFound(w, r)
				return
			}
			// Mirror the real historical 2025-07-14 archive shape: DTOP is
			// available while RP006 explicitly publishes an unavailable marker.
			if date.Format("2006-01-02") == "2026-07-08" {
				serveZip(w, "rp006.txt", `"No File Available Yet"`)
				return
			}
			serveZip(w, date.Format("20060102")+"_1_rp006-final_o.raw", rp006(cls, date))
			return
		}
		http.NotFound(w, r)
	})
	log.Printf("fake-hkex listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, h))
}

func weekend(date time.Time) bool {
	return date.Weekday() == time.Saturday || date.Weekday() == time.Sunday
}

func settlements(date time.Time) (call, put float64) {
	day := date.Day() - 1
	return 8 + float64(day)/4, 20 - float64(day)/2
}

func dtop(class string, date time.Time) string {
	call, put := settlements(date)
	business := date.Format("20060102")
	return fmt.Sprintf("\"H\",\"DTOP\",\"DCASS\",\"%s\",\"%s195046\",\"SEOCH\",1\r\n"+
		"\"01\",\"SOM\",\"STOCK OPTIONS\",\"%s\",\"17\",\"JUL\",\"26\",500.00,5000,4500,0,5000,100,%.2f,0.00,6000,5500,0,5000,100,%.2f,0.00\r\n"+
		"\"T\",2,\"EOF\"\r\n", business, business, class, call, put)
}

func rp006(class string, date time.Time) string {
	call, put := settlements(date)
	business := date.Format("20060102")
	return fmt.Sprintf("\"H\",\"RP006-FINAL\",\"DCASS\",\"%s\",\"%s195046\",\"SEOCH\",01\r\n"+
		"\"01\",\"%sSP\",\"SOM\",\"STOCK OPTIONS\",\"%s\",\"TEST UNDERLYING\",\"HKD\",500.00,500.00,0.00,\r\n"+
		"\"01\",\"%s500.00G6\",\"SOM\",\"STOCK OPTIONS\",\"%s\",\"TEST UNDERLYING\",\"HKD\",%.2f,%.2f,0.00,25.0000\r\n"+
		"\"01\",\"%s500.00S6\",\"SOM\",\"STOCK OPTIONS\",\"%s\",\"TEST UNDERLYING\",\"HKD\",%.2f,%.2f,0.00,25.0000\r\n"+
		"\"T\",3,\"EOF\"\r\n", business, business, class, class, class, class, call, call, class, class, put, put)
}

func serveZip(w http.ResponseWriter, name, body string) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := f.Write([]byte(body)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := zw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}
