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
	apiToken  = "xd" //
)

var (
	imageCache *cache.Cache
)

// authMiddleware valida el API token
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-API-Token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Se requiere API token"})
			c.Abort()
			return
		}

		if token != apiToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API token inválido"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func init() {
	// Crea los directorios de uploads y cache si no existen
	for _, dir := range []string{uploadDir, cacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Error al crear el directorio %s: %v", dir, err)
		}
	}

	// Inicializa el cache con expiración por defecto de 5 minutos y limpieza cada 10 minutos
	imageCache = cache.New(5*time.Minute, 10*time.Minute)
}

func main() {
	r := gin.Default()

	// Configura la memoria máxima para multipart
	r.MaxMultipartMemory = 8 << 20 // 8 MiB

	// Grupo protegido para subir imágenes (requiere token)
	authGroup := r.Group("/api")
	authGroup.Use(authMiddleware())
	{
		authGroup.POST("/upload", handleImageUpload)
	}

	// Endpoints públicos para servir y redimensionar imágenes (sin token)
	r.GET("/api/image/:filename", serveImage)
	r.GET("/api/resize/:filename", resizeImage)

	// Inicia el servidor
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Servidor iniciando en el puerto %s", port)
	r.Run(":" + port)
}

func handleImageUpload(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No se proporcionó imagen"})
		return
	}

	// Genera un nombre de archivo único
	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), file.Filename)
	filepath := filepath.Join(uploadDir, filename)

	if err := c.SaveUploadedFile(file, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al guardar la imagen"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Imagen subida correctamente",
		"filename": "https://cdn.nakamastream.lat/api/image/" + filename,
	})
}

func serveImage(c *gin.Context) {
	filename := c.Param("filename")
	filepath := filepath.Join(uploadDir, filename)

	// Verifica si la imagen está en cache
	if cachedPath, found := imageCache.Get(filename); found {
		c.File(cachedPath.(string))
		return
	}

	// Verifica si el archivo existe
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Imagen no encontrada"})
		return
	}

	// Guarda en cache la ruta del archivo
	imageCache.Set(filename, filepath, cache.DefaultExpiration)

	// Sirve el archivo
	c.File(filepath)
}

func resizeImage(c *gin.Context) {
	filename := c.Param("filename")
	width := c.DefaultQuery("width", "300")
	height := c.DefaultQuery("height", "300")

	// Genera una clave de cache para la imagen redimensionada
	cacheKey := fmt.Sprintf("%s_%s_%s", filename, width, height)
	
	// Verifica si la imagen redimensionada ya está en cache
	if cachedPath, found := imageCache.Get(cacheKey); found {
		c.File(cachedPath.(string))
		return
	}

	// Abre la imagen original
	srcPath := filepath.Join(uploadDir, filename)
	src, err := imaging.Open(srcPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Imagen no encontrada"})
		return
	}

	// Redimensiona la imagen
	resized := imaging.Resize(src, 300, 300, imaging.Lanczos)

	// Guarda la imagen redimensionada en el directorio de cache
	dstPath := filepath.Join(cacheDir, cacheKey)
	if err := imaging.Save(resized, dstPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al procesar la imagen"})
		return
	}

	// Guarda la ruta de la imagen redimensionada en cache
	imageCache.Set(cacheKey, dstPath, cache.DefaultExpiration)

	// Sirve la imagen redimensionada
	c.File(dstPath)
}
