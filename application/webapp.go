package main

import (
	"fmt"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/hanlders"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/jsonex"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/apploggers"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
)

func main() {
	createReleaseHandler, err := hanlders.NewCreateReleaseHandler()

	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	err = start(createReleaseHandler)

	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func start(createReleaseHandler *hanlders.CreateReleaseHandler) error {
	logger, err := apploggers.NewDevProdLogger()

	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	gin.DisableConsoleColor()
	r := gin.Default()

	r.POST("/api/octopusrelease", func(c *gin.Context) {

		applicationUpdateMessage := models.ApplicationUpdateMessage{}
		err := jsonex.DeserializeJson(c.Request.Body, &applicationUpdateMessage)

		if err != nil {
			logger.GetLogger().Error("octoargosync-init-requestbodyerror: Failed to deserialize request body: " + err.Error())

			c.JSON(http.StatusOK, models.ErrorResponse{
				Status:  "Error",
				Message: err.Error(),
			})
			return
		}

		err = createReleaseHandler.CreateRelease(applicationUpdateMessage)

		if err != nil {
			logger.GetLogger().Error("octoargosync-init-octocreatereleaseerror: Failed to create a release: " + err.Error())

			c.JSON(http.StatusOK, models.ErrorResponse{
				Status:  "Error",
				Message: err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "OK",
		})
	})

	return r.Run()
}
