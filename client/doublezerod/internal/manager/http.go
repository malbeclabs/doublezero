package manager

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
)

/*
ServeProvision handles local provisioning of a double zero tunnel. The following is an example payload:

	`{
		"user_type": "IBRL"							[required]
		"tunnel_src": "1.1.1.1", 					[optional]
		"tunnel_dst": "2.2.2.2", 					[required]
		"tunnel_net": "10.1.1.0/31",				[required]
		"doublezero_ip": "10.0.0.0",				[required]
		"doublezero_prefixes": ["10.0.0.0/24"], 	[required]
		"bgp_local_asn": 65000,						[optional]
		"bgp_remote_asn": 65001						[optional]
	}`,
*/
func (n *NetlinkManager) ServeProvision(w http.ResponseWriter, r *http.Request) {
	var p api.ProvisionRequest
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed provision request: %v"}`, err)))
		return
	}

	if p.ProgramID != n.Config.ProgramID() {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "program ID mismatch: request %s, config %s"}`, p.ProgramID.String(), n.Config.ProgramID().String())))
		return
	}

	if err = p.Validate(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "invalid request: %v"}`, err)))
		return
	}

	err = n.Provision(p)
	if err != nil {
		slog.Error("error during tunnel provisioning", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed stuff: %v"}`, err)))
		return
	}

	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (n *NetlinkManager) ServeRemove(w http.ResponseWriter, r *http.Request) {
	rr := &api.RemoveRequest{}
	err := json.NewDecoder(r.Body).Decode(rr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed provision request: %v"}`, err)))
		return
	}

	if rr.ProgramID != n.Config.ProgramID() {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "program ID mismatch: request %s, config %s"}`, rr.ProgramID.String(), n.Config.ProgramID().String())))
		return
	}

	// TODO: this is a hack until the client is updated to send user type
	if rr.UserType == api.UserTypeUnknown {
		rr.UserType = api.UserTypeIBRL
	}
	if err = rr.Validate(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "invalid request: %v"}`, err)))
		return
	}

	err = n.Remove(rr.UserType)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error during tunnel removal: %v"}`, err)))
		return
	}

	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (n *NetlinkManager) ServeStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status, err := n.Status()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error while getting status: %v"}`, err)))
		return
	}

	response := api.StatusResponse{
		ProgramID: n.Config.ProgramID(),
		Results:   status,
	}

	if err = json.NewEncoder(w).Encode(response); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error while encoding status: %v"}`, err)))
		return
	}
}
