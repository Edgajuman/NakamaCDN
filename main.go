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
	uploadDir = "./uploads" // Directory to store uploaded images
	cacheDir  = "./cache"   // Directory to store cached images
	apiToken  = "YOUR CUSTOM API TOKEN"        // API token for authentication
)

var (
	imageCache *cache.Cache // In-memory cache for storing image paths
)

// authMiddleware validates the API token for protected routes
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-API-Token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API token required"})
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
	// Create upload and cache directories if they do not exist
	for _, dir := range []string{uploadDir, cacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Error creating directory %s: %v", dir, err)
		}
	}

	// Initialize cache with a default expiration of 5 minutes and cleanup interval of 10 minutes
	imageCache = cache.New(5*time.Minute, 10*time.Minute)
}

func main() {
	r := gin.Default()

	// Set maximum memory for multipart file uploads
	r.MaxMultipartMemory = 8 << 20 // 8 MiB

	// Protected route group for image uploads (requires API token)
	authGroup := r.Group("/api")
	authGroup.Use(authMiddleware())
	{
		authGroup.POST("/upload", handleImageUpload)
	}

	// Public routes for serving and resizing images (no authentication required)
	r.GET("/api/image/:filename", serveImage)
	r.GET("/api/resize/:filename", resizeImage)

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s", port)
	r.Run(":" + port)
}

// handleImageUpload processes image uploads and saves them to the upload directory
func handleImageUpload(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No image provided"})
		return
	}

	// Generate a unique filename
	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), file.Filename)
	filepath := filepath.Join(uploadDir, filename)

	if err := c.SaveUploadedFile(file, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving image"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Image uploaded successfully",
		"filename": "https://cdn.nakamastream.lat/api/image/" + filename,
	})
}

// serveImage retrieves and serves an image from the upload directory
func serveImage(c *gin.Context) {
	filename := c.Param("filename")
	filepath := filepath.Join(uploadDir, filename)

	// Check if the image is in cache
	if cachedPath, found := imageCache.Get(filename); found {
		c.File(cachedPath.(string))
		return
	}

	// Verify if the file exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		return
	}

	// Store the file path in cache
	imageCache.Set(filename, filepath, cache.DefaultExpiration)

	// Serve the image file
	c.File(filepath)
}

// resizeImage resizes an image to specified dimensions and caches the result
func resizeImage(c *gin.Context) {
	filename := c.Param("filename")
	width := c.DefaultQuery("width", "300")
	height := c.DefaultQuery("height", "300")

	// Generate a cache key for the resized image
	cacheKey := fmt.Sprintf("%s_%s_%s", filename, width, height)

	// Check if the resized image is already cached
	if cachedPath, found := imageCache.Get(cacheKey); found {
		c.File(cachedPath.(string))
		return
	}

	// Open the original image
	srcPath := filepath.Join(uploadDir, filename)
	src, err := imaging.Open(srcPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		return
	}

	// Resize the image
	resized := imaging.Resize(src, 300, 300, imaging.Lanczos)

	// Save the resized image in the cache directory
	dstPath := filepath.Join(cacheDir, cacheKey)
	if err := imaging.Save(resized, dstPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error processing image"})
		return
	}

	// Store the resized image path in cache
	imageCache.Set(cacheKey, dstPath, cache.DefaultExpiration)

	// Serve the resized image
	c.File(dstPath)
}
