package main

import (
	"fmt"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/logging"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {

	logger, err := logging.NewDevProdLogger()

	if err != nil {
		fmt.Print("Failed to start logger")
		os.Exit(1)
	}

	gin.DisableConsoleColor()
	r := gin.Default()

	octo, err := octopus.NewLiveOctopusClient()

	if err != nil {
		logger.GetLogger().Error(err.Error())
		os.Exit(1)
	}

	extractor := domain.BodyExtractor{}

	r.POST("/api/octopusrelease", func(c *gin.Context) {

		applicationUpdateMessage, err := extractor.ExtractApplicationAndNamespace(c.Request.Body)

		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"status":  "Error",
				"message": err.Error(),
			})
		}

		octo.CreateAndDeployRelease(applicationUpdateMessage.Application, applicationUpdateMessage.Namespace)

		c.JSON(http.StatusOK, gin.H{
			"status": "OK",
		})
	})
	err = r.Run()

	if err != nil {
		os.Exit(1)
	}
}
