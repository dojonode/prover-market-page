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

// CacheData represents the structure of cached data with a timestamp
type CacheData struct {
	Timestamp int64    `json:"timestamp"`
	Data      []Prover `json:"data"`
}

// Redis caching instance
var (
	rdb *redis.Client
	ctx = context.Background()
)

const (
	MAINNET_PROVER_ENDPOINTS = "prover_endpoints"
	TESTNET_PROVER_ENDPOINTS = "testnet_prover_endpoints"
)

func main() {
	app := pocketbase.New()

	// Initialize Redis client
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Use localhost during development, redis-server points to the docker-compose redis container
		Password: "",
		DB:       0,
	})

	// serves static files from the provided public dir (if exists)
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/*", apis.StaticDirectoryHandler(os.DirFS("./pb_public"), false))
		return nil
	})

	// validProvers endpoint to return list of prover endpoints that are online
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/validProvers", func(c echo.Context) error {
			// Check cache first
			cachedData, err := rdb.Get(ctx, MAINNET_PROVER_ENDPOINTS).Result()
			if err == redis.Nil {
				// Cache miss, proceed to fetch manually
				if err := fetchAndCacheValidProvers(app, MAINNET_PROVER_ENDPOINTS); err != nil {
					return err
				}

				// Retry getting the cached data now that it is fetched
				cachedData, err = rdb.Get(ctx, MAINNET_PROVER_ENDPOINTS).Result()
				if err != nil {
					return c.JSON(http.StatusOK, []Prover{}) // Return an empty array on error
				}
			} else if err != nil {
				return err
			}

			// Cache hit, check if data is stale
			var cacheData CacheData
			if err := json.Unmarshal([]byte(cachedData), &cacheData); err != nil {
				return err
			}

			// Check if the cache is stale
			if time.Since(time.Unix(cacheData.Timestamp, 0)) > time.Hour {
				// Serve stale data and refresh the cache in the background
				go fetchAndCacheValidProvers(app, MAINNET_PROVER_ENDPOINTS)
			}

			// Serve cached data
			return c.JSON(http.StatusOK, cacheData.Data)
		})

		return nil
	})

	// validTestnetProvers endpoint to return list of prover endpoints that are online
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/validTestnetProvers", func(c echo.Context) error {
			// Check cache first
			cachedData, err := rdb.Get(ctx, TESTNET_PROVER_ENDPOINTS).Result()
			if err == redis.Nil {
				// Cache miss, proceed to fetch manually
				if err := fetchAndCacheValidProvers(app, TESTNET_PROVER_ENDPOINTS); err != nil {
					return err
				}

				// Retry getting the cached data now that it is fetched
				cachedData, err = rdb.Get(ctx, TESTNET_PROVER_ENDPOINTS).Result()
				if err != nil {
					return c.JSON(http.StatusOK, []Prover{}) // Return an empty array on error
				}
			} else if err != nil {
				return err
			}

			// Cache hit, check if data is stale
			var cacheData CacheData
			if err := json.Unmarshal([]byte(cachedData), &cacheData); err != nil {
				return err
			}

			// Check if the cache is stale
			if time.Since(time.Unix(cacheData.Timestamp, 0)) > time.Hour {
				// Serve stale data and refresh the cache in the background
				go fetchAndCacheValidProvers(app, TESTNET_PROVER_ENDPOINTS)
			}

			// Serve cached data
			return c.JSON(http.StatusOK, cacheData.Data)
		})

		return nil
	})

	// intercept create requests to check if the prover is a valid endpoint
	// fires only for "prover_endpoints" collection
	app.OnRecordBeforeCreateRequest("prover_endpoints").Add(func(e *core.RecordCreateEvent) error {
		// Get the URL value of the record
		newProverEndpoint, err := url.Parse(e.Record.GetString("url"))
		if err != nil {
			return fmt.Errorf("error parsing URL: %v", err)
		}

		// Check if prover endpoint is reachable
		validProver, err := checkProverEndpoint(newProverEndpoint.String())

		if err != nil {
			return fmt.Errorf("failed to create prover %s: %s", newProverEndpoint, err)
		}

		// Retrieve existing JSON data from Redis
		existingJSON, err := rdb.Get(ctx, MAINNET_PROVER_ENDPOINTS).Result()
		if err != nil && err != redis.Nil {
			return err
		}

		// Unmarshal existing JSON data into a slice of Prover structs
		var cacheData CacheData
		if existingJSON != "" {
			if err := json.Unmarshal([]byte(existingJSON), &cacheData); err != nil {
				return err
			}
		}

		// Append the new prover to the slice
		cacheData.Data = append(cacheData.Data, *validProver)
		cacheData.Timestamp = time.Now().Unix()

		// Marshal the updated slice back to JSON
		updatedJSON, err := json.Marshal(cacheData)
		if err != nil {
			return err
		}

		// Store the updated JSON data back to Redis
		err = rdb.Set(ctx, MAINNET_PROVER_ENDPOINTS, updatedJSON, time.Hour).Err()
		if err != nil {
			return err
		}
		log.Printf("Created the prover: %s and added to the cache\n", newProverEndpoint)

		return nil
	})
	// intercept create requests to check if the prover is a valid endpoint
	// fires only for "testnet_prover_endpoints" collection
	app.OnRecordBeforeCreateRequest("testnet_prover_endpoints").Add(func(e *core.RecordCreateEvent) error {
		// Get the URL value of the record
		newProverEndpoint, err := url.Parse(e.Record.GetString("url"))
		if err != nil {
			return fmt.Errorf("error parsing URL: %v", err)
		}

		// Check if prover endpoint is reachable
		validProver, err := checkProverEndpoint(newProverEndpoint.String())

		if err != nil {
			return fmt.Errorf("failed to create prover %s: %s", newProverEndpoint, err)
		}

		// Retrieve existing JSON data from Redis
		existingJSON, err := rdb.Get(ctx, TESTNET_PROVER_ENDPOINTS).Result()
		if err != nil && err != redis.Nil {
			return err
		}

		// Unmarshal existing JSON data into a slice of Prover structs
		var cacheData CacheData
		if existingJSON != "" {
			if err := json.Unmarshal([]byte(existingJSON), &cacheData); err != nil {
				return err
			}
		}

		// Append the new prover to the slice
		cacheData.Data = append(cacheData.Data, *validProver)
		cacheData.Timestamp = time.Now().Unix()

		// Marshal the updated slice back to JSON
		updatedJSON, err := json.Marshal(cacheData)
		if err != nil {
			return err
		}

		// Store the updated JSON data back to Redis
		err = rdb.Set(ctx, TESTNET_PROVER_ENDPOINTS, updatedJSON, time.Hour).Err()
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

func fetchAndCacheValidProvers(app *pocketbase.PocketBase, collection string) error {
	query := app.Dao().RecordQuery(collection).Limit(1000)

	records := []*models.Record{}
	if err := query.All(&records); err != nil {
		return err
	}

	// return nil when there are no records so empty array can be returned to user
	if len(records) < 1 {
		return nil
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

	// Cache the result with a new timestamp
	cacheData := CacheData{
		Timestamp: time.Now().Unix(),
		Data:      recordsResult,
	}
	data, err := json.Marshal(cacheData)
	if err != nil {
		return err
	}
	// Set cache that automatically resets after 24 hours
	err = rdb.Set(ctx, collection, data, 24*time.Hour).Err()
	if err != nil {
		return err
	}
	return nil
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
