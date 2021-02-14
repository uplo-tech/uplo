package contractor

import "github.com/uplo-tech/uplo/modules"

// Alerts implements the modules.Alerter interface for the contractor. It returns
// all alerts of the contractor.
func (c *Contractor) Alerts() (crit, err, warn []modules.Alert) {
	return c.staticAlerter.Alerts()
}
