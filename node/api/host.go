package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

var (
	// errNoPath is returned when a call fails to provide a nonempty string
	// for the path parameter.
	errNoPath = Error{"path parameter is required"}

	// errStorageFolderNotFound is returned if a call is made looking for a
	// storage folder which does not appear to exist within the storage
	// manager.
	errStorageFolderNotFound = errors.New("storage folder with the provided path could not be found")

	// ErrInvalidRPCDownloadRatio is returned if the user tries to set a value
	// for the download price or the base RPC Price that violates the maximum
	// ratio
	ErrInvalidRPCDownloadRatio = errors.New("invalid ratio between the download price and the base RPC price, base cost of 100M request should be cheaper than downloading 4TB")

	// ErrInvalidSectorAccessDownloadRatio is returned if the user tries to set
	// a value for the download price or the Sector Access Price that violates
	// the maximum ratio
	ErrInvalidSectorAccessDownloadRatio = errors.New("invalid ratio between the download price and the sector access price, base cost of 10M accesses should be cheaper than downloading 4TB")
)

type (
	// ContractInfoGET contains the information that is returned after a GET request
	// to /host/contracts - information for the host about stored obligations.
	ContractInfoGET struct {
		Contracts []modules.StorageObligation `json:"contracts"`
	}

	// HostGET contains the information that is returned after a GET request to
	// /host - a bunch of information about the status of the host.
	HostGET struct {
		ConnectabilityStatus modules.HostConnectabilityStatus `json:"connectabilitystatus"`
		ExternalSettings     modules.HostExternalSettings     `json:"externalsettings"`
		FinancialMetrics     modules.HostFinancialMetrics     `json:"financialmetrics"`
		InternalSettings     modules.HostInternalSettings     `json:"internalsettings"`
		NetworkMetrics       modules.HostNetworkMetrics       `json:"networkmetrics"`
		PriceTable           modules.RPCPriceTable            `json:"pricetable"`
		PublicKey            types.UploPublicKey               `json:"publickey"`
		WorkingStatus        modules.HostWorkingStatus        `json:"workingstatus"`
	}

	// HostEstimateScoreGET contains the information that is returned from a
	// /host/estimatescore call.
	HostEstimateScoreGET struct {
		EstimatedScore types.Currency `json:"estimatedscore"`
		ConversionRate float64        `json:"conversionrate"`
	}

	// StorageGET contains the information that is returned after a GET request
	// to /host/storage - a bunch of information about the status of storage
	// management on the host.
	StorageGET struct {
		Folders []modules.StorageFolderMetadata `json:"folders"`
	}
)

// folderIndex determines the index of the storage folder with the provided
// path.
func folderIndex(folderPath string, storageFolders []modules.StorageFolderMetadata) (int, error) {
	for _, sf := range storageFolders {
		if sf.Path == folderPath {
			return int(sf.Index), nil
		}
	}
	return -1, errStorageFolderNotFound
}

// hostContractInfoHandler handles the API call to get the contract information of the host.
// Information is retrieved via the storage obligations from the host database.
func (api *API) hostContractInfoHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	cg := ContractInfoGET{
		Contracts: api.host.StorageObligations(),
	}
	WriteJSON(w, cg)
}

// hostHandlerGET handles GET requests to the /host API endpoint, returning key
// information about the host.
func (api *API) hostHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	es := api.host.ExternalSettings()
	fm := api.host.FinancialMetrics()
	is := api.host.InternalSettings()
	nm := api.host.NetworkMetrics()
	cs := api.host.ConnectabilityStatus()
	ws := api.host.WorkingStatus()
	pk := api.host.PublicKey()
	pt := api.host.PriceTable()
	hg := HostGET{
		ConnectabilityStatus: cs,
		ExternalSettings:     es,
		FinancialMetrics:     fm,
		InternalSettings:     is,
		NetworkMetrics:       nm,
		PriceTable:           pt,
		PublicKey:            pk,
		WorkingStatus:        ws,
	}

	if api.staticDeps.Disrupt("TimeoutOnHostGET") {
		time.Sleep(httpServerTimeout + 5*time.Second)
	}

	WriteJSON(w, hg)
}

// hostsBandwidthHandlerGET handles GET requests to the /host/bandwidth API endpoint,
// returning bandwidth usage data from the host module
func (api *API) hostBandwidthHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	sent, receive, startTime, err := api.host.BandwidthCounters()
	if err != nil {
		WriteError(w, Error{"failed to get hosts's bandwidth usage: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, GatewayBandwidthGET{
		Download:  receive,
		Upload:    sent,
		StartTime: startTime,
	})
}

// parseHostSettings a request's query strings and returns a
// modules.HostInternalSettings configured with the request's query string
// parameters.
func (api *API) parseHostSettings(req *http.Request) (modules.HostInternalSettings, error) {
	settings := api.host.InternalSettings()

	if req.FormValue("acceptingcontracts") != "" {
		var x bool
		_, err := fmt.Sscan(req.FormValue("acceptingcontracts"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.AcceptingContracts = x
	}
	if req.FormValue("maxdownloadbatchsize") != "" {
		var x uint64
		_, err := fmt.Sscan(req.FormValue("maxdownloadbatchsize"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MaxDownloadBatchSize = x
	}
	if req.FormValue("maxduration") != "" {
		var x types.BlockHeight
		_, err := fmt.Sscan(req.FormValue("maxduration"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MaxDuration = x
	}
	if req.FormValue("maxrevisebatchsize") != "" {
		var x uint64
		_, err := fmt.Sscan(req.FormValue("maxrevisebatchsize"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MaxReviseBatchSize = x
	}
	if req.FormValue("netaddress") != "" {
		var x modules.NetAddress
		_, err := fmt.Sscan(req.FormValue("netaddress"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.NetAddress = x
	}
	if req.FormValue("windowsize") != "" {
		var x types.BlockHeight
		_, err := fmt.Sscan(req.FormValue("windowsize"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.WindowSize = x
	}

	if req.FormValue("collateral") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("collateral"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.Collateral = x
	}
	if req.FormValue("collateralbudget") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("collateralbudget"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.CollateralBudget = x
	}
	if req.FormValue("maxcollateral") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("maxcollateral"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MaxCollateral = x
	}

	if req.FormValue("minbaserpcprice") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("minbaserpcprice"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MinBaseRPCPrice = x
	}
	if req.FormValue("mincontractprice") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("mincontractprice"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MinContractPrice = x
	}
	if req.FormValue("mindownloadbandwidthprice") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("mindownloadbandwidthprice"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MinDownloadBandwidthPrice = x
	}
	if req.FormValue("minsectoraccessprice") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("minsectoraccessprice"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MinSectorAccessPrice = x
	}
	if req.FormValue("minstorageprice") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("minstorageprice"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MinStoragePrice = x
	}
	if req.FormValue("minuploadbandwidthprice") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("minuploadbandwidthprice"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MinUploadBandwidthPrice = x
	}
	if req.FormValue("ephemeralaccountexpiry") != "" {
		var x uint64
		_, err := fmt.Sscan(req.FormValue("ephemeralaccountexpiry"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.EphemeralAccountExpiry = time.Duration(x) * time.Second
	}
	if req.FormValue("maxephemeralaccountbalance") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("maxephemeralaccountbalance"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MaxEphemeralAccountBalance = x
	}
	if req.FormValue("maxephemeralaccountrisk") != "" {
		var x types.Currency
		_, err := fmt.Sscan(req.FormValue("maxephemeralaccountrisk"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.MaxEphemeralAccountRisk = x
	}
	if req.FormValue("registrysize") != "" {
		var x uint64
		_, err := fmt.Sscan(req.FormValue("registrysize"), &x)
		if err != nil {
			return modules.HostInternalSettings{}, err
		}
		settings.RegistrySize = x
	}
	if req.FormValue("customregistrypath") != "" {
		settings.CustomRegistryPath = req.FormValue("customregistrypath")
	}

	// Validate the RPC, Sector Access, and Download Prices
	minBaseRPCPrice := settings.MinBaseRPCPrice
	maxBaseRPCPrice := settings.MaxBaseRPCPrice()
	if minBaseRPCPrice.Cmp(maxBaseRPCPrice) > 0 {
		return modules.HostInternalSettings{}, ErrInvalidRPCDownloadRatio
	}
	minSectorAccessPrice := settings.MinSectorAccessPrice
	maxSectorAccessPrice := settings.MaxSectorAccessPrice()
	if minSectorAccessPrice.Cmp(maxSectorAccessPrice) > 0 {
		return modules.HostInternalSettings{}, ErrInvalidSectorAccessDownloadRatio
	}

	return settings, nil
}

// hostEstimateScoreGET handles the POST request to /host/estimatescore and
// computes an estimated HostDB score for the provided settings.
func (api *API) hostEstimateScoreGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// This call requires a renter, check that it is present.
	if api.renter == nil {
		WriteError(w, Error{"cannot call /host/estimatescore without the renter module"}, http.StatusBadRequest)
		return
	}

	settings, err := api.parseHostSettings(req)
	if err != nil {
		WriteError(w, Error{"error parsing host settings: " + err.Error()}, http.StatusBadRequest)
		return
	}
	var totalStorage, remainingStorage uint64
	for _, sf := range api.host.StorageFolders() {
		totalStorage += sf.Capacity
		remainingStorage += sf.CapacityRemaining
	}
	mergedSettings := modules.HostExternalSettings{
		AcceptingContracts:   settings.AcceptingContracts,
		MaxDownloadBatchSize: settings.MaxDownloadBatchSize,
		MaxDuration:          settings.MaxDuration,
		MaxReviseBatchSize:   settings.MaxReviseBatchSize,
		RemainingStorage:     remainingStorage,
		SectorSize:           modules.SectorSize,
		TotalStorage:         totalStorage,
		WindowSize:           settings.WindowSize,

		Collateral:    settings.Collateral,
		MaxCollateral: settings.MaxCollateral,

		ContractPrice:          settings.MinContractPrice,
		DownloadBandwidthPrice: settings.MinDownloadBandwidthPrice,
		StoragePrice:           settings.MinStoragePrice,
		UploadBandwidthPrice:   settings.MinUploadBandwidthPrice,

		EphemeralAccountExpiry:     settings.EphemeralAccountExpiry,
		MaxEphemeralAccountBalance: settings.MaxEphemeralAccountBalance,

		Version: build.Version,
	}
	entry := modules.HostDBEntry{}
	entry.PublicKey = api.host.PublicKey()
	entry.HostExternalSettings = mergedSettings
	// Use the default allowance for now, since we do not know what sort of
	// allowance the renters may use to attempt to access this host.
	estimatedScoreBreakdown, err := api.renter.EstimateHostScore(entry, modules.DefaultAllowance)
	if err != nil {
		WriteError(w, Error{"error estimating host score: " + err.Error()}, http.StatusInternalServerError)
		return
	}
	e := HostEstimateScoreGET{
		EstimatedScore: estimatedScoreBreakdown.Score,
		ConversionRate: estimatedScoreBreakdown.ConversionRate,
	}
	WriteJSON(w, e)
}

// hostHandlerPOST handles POST request to the /host API endpoint, which sets
// the internal settings of the host.
func (api *API) hostHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	settings, err := api.parseHostSettings(req)
	if err != nil {
		WriteError(w, Error{"error parsing host settings: " + err.Error()}, http.StatusBadRequest)
		return
	}

	err = api.host.SetInternalSettings(settings)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// hostAnnounceHandler handles the API call to get the host to announce itself
// to the network.
func (api *API) hostAnnounceHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var err error
	if addr := req.FormValue("netaddress"); addr != "" {
		err = api.host.AnnounceAddress(modules.NetAddress(addr))
	} else {
		err = api.host.Announce()
	}
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// storageHandler returns a bunch of information about storage management on
// the host.
func (api *API) storageHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	WriteJSON(w, StorageGET{
		Folders: api.host.StorageFolders(),
	})
}

// storageFoldersAddHandler adds a storage folder to the storage manager.
func (api *API) storageFoldersAddHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	folderPath := req.FormValue("path")
	var folderSize uint64
	_, err := fmt.Sscan(req.FormValue("size"), &folderSize)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	err = api.host.AddStorageFolder(folderPath, folderSize)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// storageFoldersResizeHandler resizes a storage folder in the storage manager.
func (api *API) storageFoldersResizeHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	folderPath := req.FormValue("path")
	if folderPath == "" {
		WriteError(w, Error{"path parameter is required"}, http.StatusBadRequest)
		return
	}

	storageFolders := api.host.StorageFolders()
	folderIndex, err := folderIndex(folderPath, storageFolders)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}

	var newSize uint64
	_, err = fmt.Sscan(req.FormValue("newsize"), &newSize)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	err = api.host.ResizeStorageFolder(uint16(folderIndex), newSize, false)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// storageFoldersRemoveHandler removes a storage folder from the storage
// manager.
func (api *API) storageFoldersRemoveHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	folderPath := req.FormValue("path")
	if folderPath == "" {
		WriteError(w, Error{"path parameter is required"}, http.StatusBadRequest)
		return
	}

	storageFolders := api.host.StorageFolders()
	folderIndex, err := folderIndex(folderPath, storageFolders)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}

	force := req.FormValue("force") == "true"
	err = api.host.RemoveStorageFolder(uint16(folderIndex), force)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// storageSectorsDeleteHandler handles the call to delete a sector from the
// storage manager.
func (api *API) storageSectorsDeleteHandler(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	sectorRoot, err := scanHash(ps.ByName("merkleroot"))
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	err = api.host.DeleteSector(sectorRoot)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}
