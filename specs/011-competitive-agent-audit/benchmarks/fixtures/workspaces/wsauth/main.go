package main

import (
	"fmt"
	"net/http"

	"example.com/wsauth/billing"
	"example.com/wsauth/middleware"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/price", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%.2f\n", billing.FinalPrice(100, 0.10, 0.15))
	})
	if err := http.ListenAndServe(":8080", middleware.Auth(mux)); err != nil {
		fmt.Println("server error:", err)
	}
}
