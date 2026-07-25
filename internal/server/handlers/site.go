package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	sitesvc "github.com/bestruirui/octopus/internal/site"
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
)

var syncAllSiteAccountsRunning atomic.Bool
var checkinAllSiteAccountsRunning atomic.Bool

const siteProjectionWorkers = 4

func refreshAccountRandomCheckinScheduleBestEffort(ctx context.Context, accountID int) {
	if err := sitesvc.RefreshAccountRandomCheckinSchedule(ctx, accountID); err != nil {
		log.Warnf("failed to refresh random checkin schedule (account=%d): %v", accountID, err)
	}
}

func init() {
	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listSite)).
		AddRoute(router.NewRoute("/archived", http.MethodGet).Handle(listArchivedSites)).
		AddRoute(router.NewRoute("/import/all-api-hub", http.MethodPost).Handle(importAllAPIHub)).
		AddRoute(router.NewRoute("/import/metapi", http.MethodPost).Handle(importMetAPI)).
		AddRoute(router.NewRoute("/account/sync/:id", http.MethodPost).Handle(syncSiteAccount)).
		AddRoute(router.NewRoute("/account/checkin/:id", http.MethodPost).Handle(checkinSiteAccount)).
		AddRoute(router.NewRoute("/sync-all", http.MethodPost).Handle(syncAllSiteAccounts)).
		AddRoute(router.NewRoute("/checkin-all", http.MethodPost).Handle(checkinAllSiteAccounts)).
		AddRoute(router.NewRoute("/last-sync-time", http.MethodGet).Handle(getSiteLastSyncTime)).
		AddRoute(router.NewRoute("/last-checkin-time", http.MethodGet).Handle(getSiteLastCheckinTime)).
		AddRoute(router.NewRoute("/:id/available-models", http.MethodGet).Handle(getSiteAvailableModels))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/create", http.MethodPost).Handle(createSite)).
		AddRoute(router.NewRoute("/update", http.MethodPost).Handle(updateSite)).
		AddRoute(router.NewRoute("/enable", http.MethodPost).Handle(enableSite)).
		AddRoute(router.NewRoute("/detect", http.MethodPost).Handle(detectSitePlatform)).
		AddRoute(router.NewRoute("/batch", http.MethodPost).Handle(batchSite)).
		AddRoute(router.NewRoute("/batch/edit", http.MethodPost).Handle(batchEditSite)).
		AddRoute(router.NewRoute("/account/create", http.MethodPost).Handle(createSiteAccount)).
		AddRoute(router.NewRoute("/account/update", http.MethodPost).Handle(updateSiteAccount)).
		AddRoute(router.NewRoute("/account/enable", http.MethodPost).Handle(enableSiteAccount))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/delete/:id", http.MethodDelete).Handle(deleteSite)).
		AddRoute(router.NewRoute("/archive/:id", http.MethodPost).Handle(archiveSite)).
		AddRoute(router.NewRoute("/restore/:id", http.MethodPost).Handle(restoreSite)).
		AddRoute(router.NewRoute("/account/delete/:id", http.MethodDelete).Handle(deleteSiteAccount))
}

func listSite(c *gin.Context) {
	sites, err := op.SiteList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, sites)
}

func importAllAPIHub(c *gin.Context) {
	body, err := readImportPayload(c)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	result, syncAccountIDs, err := op.SiteImportAllAPIHub(c.Request.Context(), body)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	if len(syncAccountIDs) > 0 {
		ids := append([]int(nil), syncAccountIDs...)
		safe.Go("site-import-sync", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			sitesvc.SyncAccountsWithOptions(ctx, ids, sitesync.SiteBatchOptions{Trigger: sitesync.SiteBatchTriggerImport})
		})
	}

	resp.Success(c, result)
}

func importMetAPI(c *gin.Context) {
	body, err := readImportPayload(c)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	result, err := op.SiteImportMetAPI(c.Request.Context(), body)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	resp.Success(c, result)
}

func readImportPayload(c *gin.Context) ([]byte, error) {
	limit := iolimit.ImportBodyMaxBytes()
	tooLarge := func(err error) error {
		return apperror.Wrap(op.CodeSiteImportEmptyPayload, "site import payload too large", err).WithStatus(http.StatusRequestEntityTooLarge)
	}
	if c.Request.ContentLength > limit {
		return nil, tooLarge(&iolimit.TooLargeError{Limit: limit})
	}

	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		fileHeader, err := c.FormFile("file")
		if err != nil {
			if iolimit.IsTooLarge(err) {
				return nil, tooLarge(err)
			}
			return nil, apperror.Wrap(op.CodeSiteImportEmptyPayload, "site import empty payload", err).WithStatus(http.StatusBadRequest)
		}
		file, err := fileHeader.Open()
		if err != nil {
			return nil, apperror.Wrap(op.CodeSiteImportEmptyPayload, "site import empty payload", err).WithStatus(http.StatusBadRequest)
		}
		defer file.Close()
		body, err := iolimit.ReadAll(file, limit)
		if iolimit.IsTooLarge(err) {
			return nil, tooLarge(err)
		}
		return body, err
	}
	body, err := iolimit.ReadRequestBody(c.Writer, c.Request, limit)
	if iolimit.IsTooLarge(err) {
		return nil, tooLarge(err)
	}
	return body, err
}

func createSite(c *gin.Context) {
	var site model.Site
	if err := c.ShouldBindJSON(&site); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := site.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := op.SiteCreate(&site, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, site)
}

func updateSite(c *gin.Context) {
	var req model.SiteUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	site, err := op.SiteUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	siteID := site.ID
	safe.Go("site-update-project", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := sitesvc.ProjectSite(ctx, siteID); err != nil {
			log.Warnf("background ProjectSite failed (site=%d): %v", siteID, err)
		}
	})
	resp.Success(c, site)
}

func enableSite(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.SiteEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	siteID := request.ID
	safe.Go("site-enable-project", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := sitesvc.ProjectSite(ctx, siteID); err != nil {
			log.Warnf("background ProjectSite failed (site=%d): %v", siteID, err)
		}
	})
	resp.Success(c, nil)
}

func deleteSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.DeleteSite(c.Request.Context(), idNum); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func archiveSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.ArchiveSite(c.Request.Context(), idNum); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func restoreSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.RestoreSite(c.Request.Context(), idNum); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listArchivedSites(c *gin.Context) {
	sites, err := sitesvc.ListArchivedSites(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, sites)
}

func createSiteAccount(c *gin.Context) {
	var account model.SiteAccount
	if err := c.ShouldBindJSON(&account); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := account.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := op.SiteAccountCreate(&account, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), account.ID)
	createdAccount, err := op.SiteAccountGet(account.ID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if account.Enabled && account.AutoSync {
		accountID := account.ID
		safe.Go("site-account-create-sync", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if _, err := sitesvc.SyncAccount(ctx, accountID); err != nil {
				log.Debugf("background SyncAccount failed (account=%d): %v", accountID, err)
			}
		})
	}
	resp.Success(c, createdAccount)
}

func updateSiteAccount(c *gin.Context) {
	var req model.SiteAccountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	account, err := op.SiteAccountUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), account.ID)
	account, err = op.SiteAccountGet(account.ID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	accountID := account.ID
	autoSync := account.AutoSync
	safe.Go("site-account-update-project-sync", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := sitesvc.ProjectAccount(ctx, accountID); err != nil {
			log.Warnf("background ProjectAccount failed (account=%d): %v", accountID, err)
		}
		if autoSync {
			if _, err := sitesvc.SyncAccount(ctx, accountID); err != nil {
				log.Debugf("background SyncAccount failed (account=%d): %v", accountID, err)
			}
		}
	})
	resp.Success(c, account)
}

func enableSiteAccount(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.SiteAccountEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), request.ID)
	accountID := request.ID
	safe.Go("site-account-enable-project", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := sitesvc.ProjectAccount(ctx, accountID); err != nil {
			log.Warnf("background ProjectAccount failed (account=%d): %v", accountID, err)
		}
	})
	resp.Success(c, nil)
}

func deleteSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.DeleteSiteAccount(c.Request.Context(), idNum); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func syncSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	result, err := sitesvc.SyncAccount(c.Request.Context(), idNum)
	if err != nil {
		if result != nil {
			resp.Success(c, result)
			return
		}
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, result)
}

func checkinSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	result, err := sitesvc.CheckinAccount(c.Request.Context(), idNum)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, result)
}

func syncAllSiteAccounts(c *gin.Context) {
	if !syncAllSiteAccountsRunning.CompareAndSwap(false, true) {
		resp.Error(c, http.StatusConflict, "site sync is already running")
		return
	}
	safe.Go("site-sync-all", func() {
		defer syncAllSiteAccountsRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		sitesvc.SyncAllWithOptions(ctx, sitesync.SiteBatchOptions{Trigger: sitesync.SiteBatchTriggerManual})
	})
	resp.Success(c, nil)
}

func checkinAllSiteAccounts(c *gin.Context) {
	if !checkinAllSiteAccountsRunning.CompareAndSwap(false, true) {
		resp.Error(c, http.StatusConflict, "site check-in is already running")
		return
	}
	safe.Go("site-checkin-all", func() {
		defer checkinAllSiteAccountsRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		sitesvc.CheckinAllWithOptions(ctx, sitesync.SiteBatchOptions{Trigger: sitesync.SiteBatchTriggerManual})
	})
	resp.Success(c, nil)
}

func getSiteLastSyncTime(c *gin.Context) {
	resp.Success(c, sitesvc.LastSyncAllTime())
}

func getSiteLastCheckinTime(c *gin.Context) {
	resp.Success(c, sitesvc.LastCheckinAllTime())
}

func detectSitePlatform(c *gin.Context) {
	var request struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	platform, defaultRouteType, err := sitesvc.DetectPlatform(ctx, request.URL)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result := gin.H{"platform": platform}
	if defaultRouteType != "" {
		result["default_route_type"] = defaultRouteType
	}
	resp.Success(c, result)
}

func batchSite(c *gin.Context) {
	var req model.SiteBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	validActions := map[string]bool{
		"enable": true, "disable": true, "delete": true,
	}
	if !validActions[req.Action] {
		resp.Error(c, http.StatusBadRequest, "invalid action")
		return
	}
	if len(req.IDs) == 0 {
		resp.Error(c, http.StatusBadRequest, "ids is required")
		return
	}

	result, affected, err := op.SiteBatchApply(&req, sitesvc.DeleteSite, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	projectSitesAsync(affected)
	resp.Success(c, result)
}

// projectSitesAsync 在后台逐个刷新站点投影，供批量操作成功后调用。
func projectSitesAsync(ids []int) {
	if len(ids) == 0 {
		return
	}
	siteIDs := append([]int(nil), ids...)
	safe.Go("site-batch-project", func() {
		runSiteProjectionJobs(siteIDs, siteProjectionWorkers, func(siteID int) {
			projCtx, projCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer projCancel()
			if err := sitesvc.ProjectSite(projCtx, siteID); err != nil {
				log.Warnf("background ProjectSite failed (site=%d): %v", siteID, err)
			}
		})
	})
}

func runSiteProjectionJobs(ids []int, maxConcurrency int, project func(int)) {
	if len(ids) == 0 || project == nil {
		return
	}
	if maxConcurrency <= 0 || maxConcurrency > siteProjectionWorkers {
		maxConcurrency = siteProjectionWorkers
	}
	if maxConcurrency > len(ids) {
		maxConcurrency = len(ids)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(maxConcurrency)
	for range maxConcurrency {
		safe.Go("site-batch-project-worker", func() {
			defer wg.Done()
			for siteID := range jobs {
				safe.Run("site-batch-project-item", func() {
					project(siteID)
				})
			}
		})
	}
	for _, siteID := range ids {
		jobs <- siteID
	}
	close(jobs)
	wg.Wait()
}

func batchEditSite(c *gin.Context) {
	var req model.SiteBatchEditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if len(req.IDs) == 0 {
		resp.Error(c, http.StatusBadRequest, "ids is required")
		return
	}
	req.AddTags = model.NormalizeSiteTags(req.AddTags)
	req.RemoveTags = model.NormalizeSiteTags(req.RemoveTags)
	if err := model.ValidateSiteTags(req.AddTags); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.AddTags) == 0 && len(req.RemoveTags) == 0 &&
		len(req.Upserts) == 0 && len(req.DeleteKeys) == 0 {
		resp.Error(c, http.StatusBadRequest, "nothing to edit")
		return
	}

	result, affected, err := op.SiteBatchEdit(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	projectSitesAsync(affected)
	resp.Success(c, result)
}

func getSiteAvailableModels(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	models, err := op.SiteAvailableModels(idNum, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, gin.H{"site_id": idNum, "models": models})
}
