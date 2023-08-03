package main

import (
	"fmt"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/create_release"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	createReleaseHandler, err := create_release.NewCreateReleaseHandler()

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

func start(createReleaseHandler *create_release.CreateReleaseHandler) error {
	gin.DisableConsoleColor()
	r := gin.Default()

	r.POST("/api/octopusrelease", func(c *gin.Context) {

		err := createReleaseHandler.CreateRelease(&c.Request.Body)

		if err != nil {
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
