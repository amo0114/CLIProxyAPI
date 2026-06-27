package management

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagestats"
)

var errUsageStatisticsLimit = errors.New("limit must be a positive integer")

// GetUsageStatistics returns aggregated in-memory token usage statistics.
func (h *Handler) GetUsageStatistics(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	limit, errLimit := parseUsageStatisticsLimit(c.Query("limit"))
	if errLimit != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errLimit.Error()})
		return
	}

	snapshot := usagestats.DefaultStore().Snapshot(usagestats.SnapshotOptions{
		Range: c.Query("range"),
		Limit: limit,
	})
	c.JSON(http.StatusOK, snapshot)
}

func parseUsageStatisticsLimit(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	limit, errLimit := strconv.Atoi(value)
	if errLimit != nil || limit <= 0 {
		return 0, errUsageStatisticsLimit
	}
	return limit, nil
}
