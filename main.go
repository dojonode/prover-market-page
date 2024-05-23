package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
	"github.com/redis/go-redis/v9"
)

// ResponseData represents the structure of the expected response
type ResponseData struct {
	MinSgxTierFee *int `json:"minSgxTierFee"`
}

// Prover represents a valid prover endpoint.
type Prover struct {
	URL        string `json:"url"`
	MinimumGas int    `json:"minimumGas"`
}

// Redis caching instance
var (
	rdb *redis.Client
	ctx = context.Background()
)

func main() {
	app := pocketbase.New()

	// Initialize Redis client
	rdb = redis.NewClient(&redis.Options{
		Addr:     "redis-server:6379", // Use your Redis server address
		Password: "",
		DB:       0,
	})

	// serves static files from the provided public dir (if exists)
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/*", apis.StaticDirectoryHandler(os.DirFS("./pb_public"), false))
		return nil
	})

	// /validProvers endpoint to return list of prover endpoints that are online
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/validProvers", func(c echo.Context) error {
			// Check time
			// start := time.Now()

			// Check cache first
			cachedData, err := rdb.Get(ctx, "validProvers").Result()
			if err == redis.Nil {
				// Cache miss, proceed to fetch from DB
				query := app.Dao().RecordQuery("prover_endpoints").Limit(1000)

				records := []*models.Record{}
				if err := query.All(&records); err != nil {
					return err
				}

				var recordsResult []Prover

				// Loop through the records and store the endpoints that are available in recordsResult

				for _, record := range records {
					validProver, err := checkProverEndpoint(record.GetString("url"))
					if err != nil {
						continue
					}
					if validProver != nil {
						recordsResult = append(recordsResult, *validProver)
					}
				}

				// Cache the result
				data, err := json.Marshal(recordsResult)
				if err != nil {
					return err
				}
				err = rdb.Set(ctx, "validProvers", data, time.Hour).Err()
				if err != nil {
					return err
				}
				// timeElapsed := time.Since(start)
				// fmt.Printf("Uncached hit, this took a while: %fs\n", timeElapsed.Seconds())
				return c.JSON(http.StatusOK, recordsResult)
			} else if err != nil {
				return err
			}

			// Cache hit, return cached data
			var recordsResult []Prover
			if err := json.Unmarshal([]byte(cachedData), &recordsResult); err != nil {
				return err
			}
			// timeElapsed := time.Since(start)
			// fmt.Printf("Cache hit, this was fast: %fs\n", timeElapsed.Seconds())
			return c.JSON(http.StatusOK, recordsResult)
		} /* optional middlewares */)

		return nil
	})

	// intercept create requests to check if the prover is a valid endpoint
	// fires only for "prover_endpoints" collection
	app.OnRecordBeforeCreateRequest("prover_endpoints").Add(func(e *core.RecordCreateEvent) error {
		// Get the URL value of the record
		newProverEndpoint, err := url.Parse(e.Record.GetString("url"))
		if err != nil {
			return fmt.Errorf("Error parsing URL: %v\n", err)
		}

		// Check if prover endpoint is reachable
		validProver, err := checkProverEndpoint(newProverEndpoint.String())

		if err != nil {
			return fmt.Errorf("Failed to create prover %s: %s", newProverEndpoint, err)
		}

		// Retrieve existing JSON data from Redis
		existingJSON, err := rdb.Get(ctx, "validProvers").Result()
		if err != nil && err != redis.Nil {
			return err
		}

		// Unmarshal existing JSON data into a slice of Prover structs
		var existingProvers []Prover
		if existingJSON != "" {
			if err := json.Unmarshal([]byte(existingJSON), &existingProvers); err != nil {
				return err
			}
		}

		// Append the new prover to the slice
		existingProvers = append(existingProvers, *validProver)

		// Marshal the updated slice back to JSON
		updatedJSON, err := json.Marshal(existingProvers)
		if err != nil {
			return err
		}

		// Store the updated JSON data back to Redis
		err = rdb.Set(ctx, "validProvers", updatedJSON, time.Hour).Err()
		if err != nil {
			return err
		}
		log.Printf("Created the prover: %s and added to the cache\n", newProverEndpoint)

		return nil
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func checkProverEndpoint(url string) (*Prover, error) {
	fullUrl := url + "/status"

	client := &http.Client{Timeout: 4 * time.Second}

	req, err := http.NewRequest("GET", fullUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-OK HTTP status: %s", resp.Status)
	}

	var data ResponseData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("error decoding response body: %v", err)
	}

	if data.MinSgxTierFee != nil {

		return &Prover{
			URL:        url,
			MinimumGas: *data.MinSgxTierFee,
		}, nil
	}

	return nil, nil
}
