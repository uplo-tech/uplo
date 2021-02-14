package gateway

import (
	"os"
	"path/filepath"
	"time"

	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
)

const (
	// logFile is the name of the log file.
	logFile = modules.GatewayDir + ".log"

	// persistFilename is the filename to be used when persisting gateway information to a JSON file
	persistFilename = "gateway.json"
)

// persistMetadata contains the header and version strings that identify the
// gateway persist file.
var persistMetadata = persist.Metadata{
	Header:  "Gateway Persistence",
	Version: "1.5.0",
}

type (
	// persist contains all of the persistent gateway data.
	persistence struct {
		RouterURL string

		// rate limit settings
		MaxDownloadSpeed int64
		MaxUploadSpeed   int64

		// blocklisted IPs
		Blocklist []string
	}
)

// nodePersistData returns the node data in the Gateway that will be saved to disk.
func (g *Gateway) nodePersistData() (nodes []*node) {
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}
	return
}

// load loads the Gateway's persistent data from disk.
func (g *Gateway) load() error {
	// load nodes
	var nodes []*node
	var v130 bool
	err := persist.LoadJSON(nodePersistMetadata, &nodes, filepath.Join(g.persistDir, nodesFile))
	if err != nil && !os.IsNotExist(err) {
		// COMPATv1.3.0
		compatErr := g.loadv033persist()
		if compatErr != nil {
			return err
		}
		v130 = true
	}
	for i := range nodes {
		g.nodes[nodes[i].NetAddress] = nodes[i]
	}

	// If we were loading a 1.3.0 gateway we are done. It doesn't have a
	// gateway.json.
	if v130 {
		return nil
	}

	// load g.persist
	err = persist.LoadJSON(persistMetadata, &g.persist, filepath.Join(g.persistDir, persistFilename))
	if os.IsNotExist(err) {
		// There is no gateway.json, nothing to load.
		return nil
	}
	if errors.Contains(err, persist.ErrBadVersion) {
		// Try update the version of the metadata
		err = g.convertPersistv135Tov150()
		if err != nil {
			return errors.AddContext(err, "failed to convert persistence from v135 to v150")
		}
		// Load the new persistence
		err = persist.LoadJSON(persistMetadata, &g.persist, filepath.Join(g.persistDir, persistFilename))
	}
	if err != nil {
		return errors.AddContext(err, "failed to load gateway persistence")
	}
	// create map from blocklist
	for _, ip := range g.persist.Blocklist {
		g.blocklist[ip] = struct{}{}
	}
	return nil
}

// saveSync stores the Gateway's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (g *Gateway) saveSync() error {
	g.persist.Blocklist = make([]string, 0, len(g.blocklist))
	for ip := range g.blocklist {
		g.persist.Blocklist = append(g.persist.Blocklist, ip)
	}
	return persist.SaveJSON(persistMetadata, g.persist, filepath.Join(g.persistDir, persistFilename))
}

// saveSyncNodes stores the Gateway's persistent node data on disk, and then
// syncs to disk to minimize the possibility of data loss.
func (g *Gateway) saveSyncNodes() error {
	return persist.SaveJSON(nodePersistMetadata, g.nodePersistData(), filepath.Join(g.persistDir, nodesFile))
}

// threadedSaveLoop periodically saves the gateway nodes.
func (g *Gateway) threadedSaveLoop() {
	for {
		select {
		case <-g.threads.StopChan():
			return
		case <-time.After(saveFrequency):
		}

		func() {
			err := g.threads.Add()
			if err != nil {
				return
			}
			defer g.threads.Done()

			g.mu.Lock()
			err = g.saveSyncNodes()
			g.mu.Unlock()
			if err != nil {
				g.log.Println("ERROR: Unable to save gateway nodes:", err)
			}
		}()
	}
}

// loadv033persist loads the v0.3.3 Gateway's persistent node data from disk.
func (g *Gateway) loadv033persist() error {
	var nodes []modules.NetAddress
	err := persist.LoadJSON(persist.Metadata{
		Header:  "Uplo Node List",
		Version: "0.3.3",
	}, &nodes, filepath.Join(g.persistDir, nodesFile))
	if err != nil {
		return err
	}
	for _, addr := range nodes {
		err := g.addNode(addr)
		if err != nil {
			g.log.Printf("WARN: error loading node '%v' from persist: %v", addr, err)
		}
	}
	return nil
}
