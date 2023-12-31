package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/zmb3/spotify/v2"

	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	currentDevice    spotify.PlayerDevice
	fallbackPlaylist FallbackPlaylist
	db               *gorm.DB
	auth             *spotifyauth.Authenticator
	minimumVotes     int64
	currentTrackURI  spotify.URI
	client           *spotify.Client
	oauthToken       LoginToken
)

var (
	state          = "spotifyJukeBox"
	pollingSpotify = false
)

func main() {
	// Load Env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Load Database & Migrate the schema
	db, err = gorm.Open(sqlite.Open(os.Getenv("DB_PATH")), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}
	db.AutoMigrate(&Track{})
	db.AutoMigrate(&TrackImage{})
	db.AutoMigrate(&Device{})
	db.AutoMigrate(&LoginToken{})

	// Set Rate Limiting
	rateLimit, _ := strconv.ParseUint(os.Getenv("MAXIMUM_VOTES_PER_HOUR"), 10, 32)
	store := ratelimit.InMemoryStore(&ratelimit.InMemoryOptions{
		Rate:  time.Hour,
		Limit: uint(rateLimit),
	})

	rateLimitMiddleWare := ratelimit.RateLimiter(store, &ratelimit.Options{
		ErrorHandler: func(c *gin.Context, info ratelimit.Info) {
			c.JSON(429, "Too many requests. Try again in "+time.Until(info.ResetTime).String())
		},
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP() + c.Request.UserAgent()
		},
	})

	// Load Spotify API
	auth = spotifyauth.New(
		spotifyauth.WithRedirectURL(os.Getenv("CALLBACK_URL")),
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserReadCurrentlyPlaying,
			spotifyauth.ScopeUserReadPlaybackState,
			spotifyauth.ScopeUserModifyPlaybackState,
			spotifyauth.ScopePlaylistModifyPrivate,
			spotifyauth.ScopePlaylistModifyPublic,
		),
	)

	// Set Spotify Login Token
	dbLoginToken := LoginToken{}
	if err := db.First(&dbLoginToken).Error; err != nil {
		// Assume no Login is Set
		log.Println("-------------")
		log.Println("NO LOGIN SET")
		log.Println("-------------")
	} else {
		oauthToken.AccessToken = dbLoginToken.AccessToken
		oauthToken.TokenType = dbLoginToken.TokenType
		oauthToken.RefreshToken = dbLoginToken.RefreshToken
		oauthToken.Expiry = dbLoginToken.Expiry
		client = spotify.New(auth.Client(context.TODO(), &oauth2.Token{
			AccessToken:  dbLoginToken.AccessToken,
			TokenType:    dbLoginToken.TokenType,
			RefreshToken: dbLoginToken.RefreshToken,
			Expiry:       dbLoginToken.Expiry,
		}))
		log.Println("-------------")
		log.Println("LOGIN SET")
		log.Println("-------------")
	}

	// Set Device ID
	dbDevice := Device{}
	if err := db.First(&dbDevice).Error; err != nil {
		// Assume no Device is Set
		log.Println("-------------")
		log.Println("NO DEVICE SET")
		log.Println("-------------")
	} else {
		currentDevice.ID = dbDevice.ID
		currentDevice.Active = false
		currentDevice.Name = dbDevice.Name
		currentDevice.Type = dbDevice.Type
		log.Println("-------------")
		log.Println("DEVICE SET")
		log.Println(dbDevice.Name)
		log.Println("-------------")
	}

	// Set Fallback Playlist
	addToPlaylist, _ := strconv.ParseBool(os.Getenv("FALLBACK_PLAYLIST_ADD_QUEUED"))
	fallbackPlaylist = FallbackPlaylist{
		URI:           spotify.URI(os.Getenv("FALLBACK_PLAYLIST_URI")),
		ID:            spotify.ID(strings.Replace(os.Getenv("FALLBACK_PLAYLIST_URI"), "spotify:playlist:", "", -1)),
		Active:        false,
		AddToPlaylist: addToPlaylist,
	}

	// Set Minimum Votes
	minimumVotes, _ = strconv.ParseInt(os.Getenv("MINIMUM_VOTES_TO_REMOVE"), 10, 64)

	// Set Logging to file
	logToFile, _ := strconv.ParseBool(os.Getenv("APP_LOG_TO_FILE"))
	if logToFile {
		file, err := os.OpenFile(os.Getenv("APP_LOG_PATH"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(file)
	}

	// Start Router
	r := gin.Default()

	r.Use(cors.Default())

	authorized := r.Group("", gin.BasicAuth(gin.Accounts{
		"admin": os.Getenv("ADMIN_PASSWORD"),
	}))

	// Set Routes
	r.GET("/search/:searchTerm", handleSearch)

	r.POST("/votes/:action", rateLimitMiddleWare, handleVote)

	r.GET("/auth/callback", handleAuth)
	r.GET("/auth/login", serveLoginLink)

	authorized.POST("/player/:action", handlePlayer)

	r.GET("/tracks", getTracks)
	r.GET("/tracks/current", getTrackCurrent)
	r.GET("/tracks/:trackUri", getTrackByUri)
	r.POST("/tracks/:action", handleTrack)
	authorized.POST("/tracks/remove", removeTrack)

	authorized.GET("/device/all", getAllDevices)
	authorized.GET("/device", getCurrentDevice)
	authorized.POST("/device", setDevice)

	r.Run(":8888")
}
