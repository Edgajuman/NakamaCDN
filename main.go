// Package main implements an image upload and processing service with caching capabilities.
// It provides REST API endpoints for image upload, serving, and resizing operations.
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

// Constants define the application's configuration parameters
const (
	uploadDir = "./uploads" // Directory to store uploaded images
	cacheDir  = "./cache"   // Directory to store resized/processed images
	apiToken  = "YOUR CUSTOM API TOKEN"        // Authentication token for protected routes
)

// Global variables
var (
	imageCache *cache.Cache // In-memory cache for optimizing image serving performance
)

// authMiddleware implements API token validation for protected routes.
// It checks for the presence and validity of the X-API-Token header.
// Returns 401 Unauthorized if the token is missing or invalid.
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

// init initializes the application by setting up required directories
// and configuring the in-memory cache with specified expiration times.
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

// main is the entry point of the application.
// It sets up the HTTP server, configures routes, and starts listening for requests.
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

// handleImageUpload processes incoming image upload requests.
// It:
// - Validates the uploaded file
// - Generates a unique filename
// - Saves the file to the upload directory
// - Returns the public URL for accessing the image
// Returns 400 Bad Request if no image is provided
// Returns 500 Internal Server Error if the save operation fails
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

// serveImage handles requests for retrieving uploaded images.
// It implements a caching mechanism to improve performance for frequently accessed images.
// Features:
// - Checks in-memory cache first
// - Verifies file existence
// - Caches file paths for subsequent requests
// Returns 404 Not Found if the requested image doesn't exist
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

// resizeImage handles image resizing requests with caching.
// Capabilities:
// - Resizes images to specified dimensions (defaults to 300x300)
// - Caches resized versions to avoid redundant processing
// - Uses high-quality Lanczos resampling
// Parameters:
// - width: desired width (optional, default: 300)
// - height: desired height (optional, default: 300)
// Returns 404 Not Found if the source image doesn't exist
// Returns 500 Internal Server Error if resizing fails
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

	// Resized image
	c.File(dstPath)
}
