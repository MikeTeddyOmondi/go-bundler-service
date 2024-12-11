package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"dagger.io/dagger"
	"github.com/gin-gonic/gin"

	amqp "github.com/rabbitmq/amqp091-go"
)

type QueueMessage struct {
	ImageName string `json:"image_name"`
	UniqueID  string `json:"unique_id"`
	// ... other fields
}

type book struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Quantity int    `json:"quantity"`
}

var books = []book{
	{ID: "1", Title: "In Search of Lost Time", Author: "Marcel Proust", Quantity: 2},
	{ID: "2", Title: "The Great Gatsby", Author: "F. Scott Fitzgerald", Quantity: 5},
	{ID: "3", Title: "War and Peace", Author: "Leo Tolstoy", Quantity: 6},
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", msg, err)
	}
}

func getBooks(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, books)
}

func bookById(c *gin.Context) {
	id := c.Param("id")
	book, err := getBookById(id)

	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "Book not found."})
		return
	}

	c.IndentedJSON(http.StatusOK, book)
}

func checkoutBook(c *gin.Context) {
	id, ok := c.GetQuery("id")

	if !ok {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "Missing id query parameter."})
		return
	}

	book, err := getBookById(id)

	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "Book not found."})
		return
	}

	if book.Quantity <= 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "Book not available."})
		return
	}

	book.Quantity -= 1
	c.IndentedJSON(http.StatusOK, book)
}

func returnBook(c *gin.Context) {
	id, ok := c.GetQuery("id")

	if !ok {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "Missing id query parameter."})
		return
	}

	book, err := getBookById(id)

	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "Book not found."})
		return
	}

	book.Quantity += 1
	c.IndentedJSON(http.StatusOK, book)
}

func getBookById(id string) (*book, error) {
	for i, b := range books {
		if b.ID == id {
			return &books[i], nil
		}
	}

	return nil, errors.New("book not found")
}

func createBook(c *gin.Context) {
	var newBook book

	if err := c.BindJSON(&newBook); err != nil {
		return
	}

	books = append(books, newBook)
	c.IndentedJSON(http.StatusCreated, newBook)
}

func bundleReact(c *gin.Context) {
	ctx := context.Background()

	// client, err := dagger.Connect(ctx)
	// if err != nil {
	// 	panic(err)
	// }
	// defer client.Close()

	// _, err = client.Git("https://github.com/MikeTeddyOmondi/bun-next-app").
	// 	Branch("main").
	// 	Tree().
	// 	Entries(ctx)
	// if err != nil {
	// 	panic(err)
	// }
	// // fmt.Println(gitRepo)

	// nodeCacheKey := "npm-install-cache"

	// nodeCache := client.CacheVolume(nodeCacheKey)

	// nodeImage := client.Container().From("node:18.18.2-alpine").
	// 	WithMountedCache("/cache", nodeCache)

	// // runner := nodeImage.WithWorkdir("/src").
	// runner := nodeImage.
	// 	WithDirectory("/app", client.Git("https://github.com/MikeTeddyOmondi/bun-next-app").
	// 		Branch("main").
	// 		Tree()).
	// 	WithExec([]string{"npm", "install"})

	// runner.WithExec([]string{"npm", "run", "build"})

	// dir, err := runner.Directory("/build").Export(ctx, "./tmp/build")
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println(dir)

	// Improved
	// ___________
	// Create a new Dagger client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stdout), dagger.WithRunnerHost("docker-container://dagger-engine-v0.14.0"))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Define the GitHub repository
	repo := client.Git("https://github.com/MikeTeddyOmondi/bun-next-app")
	repoEntries, err := repo.Branch("main").Tree().Entries(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("Repo files: ", repoEntries)

	// Multi-stage Docker build
	builder := client.Container().
		From("node:18.18.2-alpine").
		WithDirectory("/app", repo.Branch("main").Tree()).
		WithWorkdir("/app").
		WithExec([]string{"npm", "install"}).
		WithExec([]string{"npm", "run", "build"})

	// Build the production image
	prodImage := client.Container().
		From("nginx:alpine").
		WithDirectory("/usr/share/nginx/html", builder.Directory("/app/dist")).
		WithEntrypoint([]string{"/usr/sbin/nginx", "-g", "daemon off;"})

	// Push the image to Docker Hub
	timeId := time.Now().UnixNano()
	builtImage, err := prodImage.Publish(ctx, fmt.Sprintf("ttl.sh/locci-build-%d:1h", timeId))
	if err != nil {
		panic(err)
	}
	fmt.Println(builtImage)

	// Export the built image to a local directory
	// Create a unique build directory name using a timestamp or UUID.
	buildDir := fmt.Sprintf("./tmp/locci/build-%d", timeId)

	// Ensure the parent directory exists.
	if err := os.MkdirAll(buildDir, os.ModePerm); err != nil {
		fmt.Printf("failed to create build directory:  %v\n", err)
		return
	}
	dir, err := builder.Directory("/app/dist").Export(ctx, buildDir)
	if err != nil {
		panic(err)
	}
	fmt.Println("Pipeline completed successfully!", dir)

	rabbitMQ, err := amqp.Dial("amqp://user:password@localhost:5672/")
	failOnError(err, "Failed to connect to RabbitMQ")
	defer rabbitMQ.Close()

	ch, err := rabbitMQ.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	queue, err := ch.QueueDeclare(
		"locci-deploy", // name
		false,          // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	failOnError(err, "Failed to declare a queue")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Marshal the struct to JSON bytes
	// body := "Hello World!"
	queueMsg := QueueMessage{
		ImageName: builtImage,
		UniqueID: fmt.Sprintf("%d", timeId),
	}
	jsonBytes, err := json.Marshal(queueMsg)
	if err != nil {
		fmt.Printf("error marshaling message: %v", err)
		return
	}

	err = ch.PublishWithContext(ctx,
		"",         // exchange
		queue.Name, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        jsonBytes,
		})
	failOnError(err, "Failed to publish a message")
	log.Printf("[x] Sent %s\n", jsonBytes)

	buildResponse := gin.H{
		"data": queueMsg,
	}

	c.IndentedJSON(http.StatusOK, buildResponse)
}

func main() {
	router := gin.Default()
	router.POST("/build-react", bundleReact)
	router.GET("/books", getBooks)
	router.GET("/books/:id", bookById)
	router.POST("/books", createBook)
	router.PATCH("/checkout", checkoutBook)
	router.PATCH("/return", returnBook)
	router.Run("localhost:8080")
}
