package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/disintegration/imaging"
	"github.com/patrickmn/go-cache"
)

const (
	uploadDir = "./uploads"
	cacheDir  = "./cache"
	apiToken  = "rosca" // In production, this should be in environment variables
)

var (
	imageCache *cache.Cache
)

// authMiddleware validates the API token
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-API-Token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API token is required"})
			c.Abort()
			return
		}

		if token != apiToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func init() {
	// Create upload and cache directories if they don't exist
	for _, dir := range []string{uploadDir, cacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Initialize cache with 5 minutes default expiration and 10 minutes cleanup interval
	imageCache = cache.New(5*time.Minute, 10*time.Minute)
}

func main() {
	r := gin.Default()

	// Set maximum multipart memory
	r.MaxMultipartMemory = 8 << 20 // 8 MiB

	// Create an API route group with authentication
	api := r.Group("/api")
	api.Use(authMiddleware())
	{
		// Protected routes
		api.POST("/upload", handleImageUpload)
		api.GET("/image/:filename", serveImage)
		api.GET("/resize/:filename", resizeImage)
	}

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s", port)
	r.Run(":" + port)
}

func handleImageUpload(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No image provided"})
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), file.Filename)
	filepath := filepath.Join(uploadDir, filename)

	if err := c.SaveUploadedFile(file, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Image uploaded successfully",
		"filename": filename,
	})
}

func serveImage(c *gin.Context) {
	filename := c.Param("filename")
	filepath := filepath.Join(uploadDir, filename)

	// Check if file exists in cache
	if cachedPath, found := imageCache.Get(filename); found {
		c.File(cachedPath.(string))
		return
	}

	// Check if file exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		return
	}

	// Cache the file path
	imageCache.Set(filename, filepath, cache.DefaultExpiration)

	// Serve the file
	c.File(filepath)
}

func resizeImage(c *gin.Context) {
	filename := c.Param("filename")
	width := c.DefaultQuery("width", "300")
	height := c.DefaultQuery("height", "300")

	// Generate cache key
	cacheKey := fmt.Sprintf("%s_%s_%s", filename, width, height)
	
	// Check cache first
	if cachedPath, found := imageCache.Get(cacheKey); found {
		c.File(cachedPath.(string))
		return
	}

	// Open original image
	srcPath := filepath.Join(uploadDir, filename)
	src, err := imaging.Open(srcPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		return
	}

	// Resize image
	resized := imaging.Resize(src, 300, 300, imaging.Lanczos)

	// Save resized image to cache directory
	dstPath := filepath.Join(cacheDir, cacheKey)
	if err := imaging.Save(resized, dstPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process image"})
		return
	}

	// Cache the resized image path
	imageCache.Set(cacheKey, dstPath, cache.DefaultExpiration)

	// Serve the resized image
	c.File(dstPath)
}
