package githubapi

import (
	"net/http"
	"os"
)

func SetAuthHeader(req *http.Request) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		return
	}

	req.Header.Set("Authorization", "Bearer "+token)
}
