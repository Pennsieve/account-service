package account

import errs "errors"

// ErrLegacyRegistrationRetired refuses a registration without an external ID.
// Accounts are connected in the web app, whose role setup issues one;
// accounts registered before external IDs existed keep working.
var ErrLegacyRegistrationRetired = errs.New("Registering without an external ID is no longer supported. Connect the account in the Pennsieve web app: Analysis → Compute Nodes → Connect AWS account.")
