package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zmb3/spotify/v2"
)

func serveLoginLink(c *gin.Context) {
	url := auth.AuthURL(state)
	c.JSON(http.StatusOK, url)
}

func handleAuth(c *gin.Context) {
	tok, err := auth.Token(c.Request.Context(), state, c.Request)
	if err != nil {
		log.Fatal(err)
		c.JSON(http.StatusInternalServerError, err)
	}
	if st := c.Request.FormValue("state"); st != state {
		log.Fatalf("State mismatch: %s != %s\n", st, state)
		c.JSON(http.StatusNotFound, err)
	}
	client = spotify.New(auth.Client(c.Request.Context(), tok))

	// Set OAuth Token
	oauthToken.AccessToken = tok.AccessToken
	oauthToken.TokenType = tok.TokenType
	oauthToken.RefreshToken = tok.RefreshToken
	oauthToken.Expiry = tok.Expiry

	// ch <- client

	// Return Auth to client
	c.JSON(http.StatusOK, oauthToken)
}
