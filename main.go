package main

import (
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

func main() {
	app := pocketbase.New()

	// serves static files from the provided public dir (if exists)
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/*", apis.StaticDirectoryHandler(os.DirFS("./pb_public"), false))
		return nil
	})

	// /validProvers endpoint to return list of prover endpoints that are online
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.Router.GET("/validProvers", func(c echo.Context) error {
			query := app.Dao().RecordQuery("prover_endpoints").
				Limit(1000)

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
		_, err = checkProverEndpoint(newProverEndpoint.String())

		if err != nil {
			return fmt.Errorf("Failed to create prover %s: %s", newProverEndpoint, err)
		}
		log.Println("Creating the prover: ", newProverEndpoint)
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
