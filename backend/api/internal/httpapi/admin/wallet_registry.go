package adminapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
	"time"
)

type walletRegistryDTO struct {
	Chain                 string          `json:"chain"`
	Address               string          `json:"address"`
	EntityID              *string         `json:"entityId,omitempty"`
	EntityName            string          `json:"entityName"`
	EntityType            string          `json:"entityType"`
	Status                string          `json:"status"`
	LabelSource           string          `json:"labelSource"`
	VerificationSourceURL string          `json:"verificationSourceUrl"`
	Notes                 string          `json:"notes"`
	Aliases               json.RawMessage `json:"aliases"`
	FirstSeenAt           string          `json:"firstSeenAt"`
	LastSeenAt            string          `json:"lastSeenAt"`
	SignificantTxCount    int             `json:"significantTxCount"`
	LargestTxUSD          float64         `json:"largestTxUsd"`
	KnownBalanceUSD       float64         `json:"knownBalanceUsd"`
	SampleTxHashes        json.RawMessage `json:"sampleTxHashes"`
	Counterparties        json.RawMessage `json:"counterparties"`
	SuggestedEntityType   string          `json:"suggestedEntityType"`
	ExplorerURL           string          `json:"explorerUrl"`
	ArkhamURL             string          `json:"arkhamUrl"`
}
type walletPatch struct {
	EntityName            *string  `json:"entityName"`
	EntityType            *string  `json:"entityType"`
	Status                *string  `json:"status"`
	Notes                 *string  `json:"notes"`
	VerificationSourceURL *string  `json:"verificationSourceUrl"`
	EntityID              *string  `json:"entityId"`
	KnownBalanceUSD       *float64 `json:"knownBalanceUsd"`
}

type walletCreate struct {
	Chain                 string  `json:"chain"`
	Address               string  `json:"address"`
	EntityName            string  `json:"entityName"`
	EntityType            string  `json:"entityType"`
	Status                string  `json:"status"`
	Notes                 string  `json:"notes"`
	VerificationSourceURL string  `json:"verificationSourceUrl"`
	KnownBalanceUSD       float64 `json:"knownBalanceUsd"`
}

func (a *API) handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	var body walletCreate
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	body.Chain = strings.ToLower(strings.TrimSpace(body.Chain))
	body.Address = strings.ToLower(strings.TrimSpace(body.Address))
	body.EntityName = strings.TrimSpace(body.EntityName)
	body.EntityType = strings.TrimSpace(body.EntityType)
	body.Status = strings.ToLower(strings.TrimSpace(body.Status))
	if body.Chain == "" || body.Address == "" || body.EntityName == "" {
		writeErr(w, http.StatusBadRequest, "chain_address_and_name_required")
		return
	}
	if body.Status == "" {
		body.Status = "probable"
	}
	if body.Status != "verified" && body.Status != "probable" && body.Status != "unknown" && body.Status != "ignore" {
		writeErr(w, http.StatusBadRequest, "invalid_status")
		return
	}
	if body.EntityType == "" {
		body.EntityType = "unknown"
	}
	_, err := a.db.Exec(r.Context(), `
		INSERT INTO wallet_registry (
			chain,address,entity_id,entity_name,entity_type,status,label_source,
			verification_source_url,notes,known_balance_usd,first_seen_at,last_seen_at
		) VALUES ($1,$2,gen_random_uuid(),$3,$4,$5,'admin',$6,$7,$8,now(),now())
		ON CONFLICT (chain,address) DO UPDATE SET
			entity_id=COALESCE(wallet_registry.entity_id,gen_random_uuid()),
			entity_name=EXCLUDED.entity_name,entity_type=EXCLUDED.entity_type,
			status=EXCLUDED.status,label_source='admin',
			verification_source_url=EXCLUDED.verification_source_url,
			notes=EXCLUDED.notes,known_balance_usd=EXCLUDED.known_balance_usd,last_seen_at=now(),updated_at=now()
	`, body.Chain, body.Address, body.EntityName, body.EntityType, body.Status, body.VerificationSourceURL, body.Notes, nonNegative(body.KnownBalanceUSD))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"chain": body.Chain, "address": body.Address})
}

func (a *API) handleListWalletRegistry(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	chain := strings.TrimSpace(r.URL.Query().Get("chain"))
	limit := 100
	rows, err := a.db.Query(r.Context(), `SELECT chain,address,entity_id,entity_name,entity_type,status,label_source,verification_source_url,notes,aliases,first_seen_at,last_seen_at,significant_tx_count,largest_tx_usd,known_balance_usd,sample_tx_hashes,counterparties,suggested_entity_type FROM wallet_registry WHERE ($1='' OR chain=$1) AND ($2='' OR status=$2) AND ($3='' OR address ILIKE '%'||$3||'%' OR entity_name ILIKE '%'||$3||'%') ORDER BY largest_tx_usd DESC,last_seen_at DESC LIMIT $4`, chain, status, q, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()
	out := []walletRegistryDTO{}
	for rows.Next() {
		var d walletRegistryDTO
		var id *string
		var first, last time.Time
		if err := rows.Scan(&d.Chain, &d.Address, &id, &d.EntityName, &d.EntityType, &d.Status, &d.LabelSource, &d.VerificationSourceURL, &d.Notes, &d.Aliases, &first, &last, &d.SignificantTxCount, &d.LargestTxUSD, &d.KnownBalanceUSD, &d.SampleTxHashes, &d.Counterparties, &d.SuggestedEntityType); err != nil {
			writeErr(w, http.StatusInternalServerError, "db_error")
			return
		}
		d.EntityID = id
		d.FirstSeenAt = first.UTC().Format(time.RFC3339)
		d.LastSeenAt = last.UTC().Format(time.RFC3339)
		d.ExplorerURL = explorerAddress(d.Chain, d.Address)
		d.ArkhamURL = "https://arkhamintelligence.com/explorer/address/" + d.Address
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (a *API) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	chain, address := chi.URLParam(r, "chain"), strings.ToLower(chi.URLParam(r, "address"))
	var d walletRegistryDTO
	var id *string
	var first, last time.Time
	err := a.db.QueryRow(r.Context(), `SELECT chain,address,entity_id,entity_name,entity_type,status,label_source,verification_source_url,notes,aliases,first_seen_at,last_seen_at,significant_tx_count,largest_tx_usd,known_balance_usd,sample_tx_hashes,counterparties,suggested_entity_type FROM wallet_registry WHERE chain=$1 AND address=$2`, chain, address).Scan(&d.Chain, &d.Address, &id, &d.EntityName, &d.EntityType, &d.Status, &d.LabelSource, &d.VerificationSourceURL, &d.Notes, &d.Aliases, &first, &last, &d.SignificantTxCount, &d.LargestTxUSD, &d.KnownBalanceUSD, &d.SampleTxHashes, &d.Counterparties, &d.SuggestedEntityType)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	d.EntityID = id
	d.FirstSeenAt = first.UTC().Format(time.RFC3339)
	d.LastSeenAt = last.UTC().Format(time.RFC3339)
	d.ExplorerURL = explorerAddress(chain, address)
	d.ArkhamURL = "https://arkhamintelligence.com/explorer/address/" + address
	writeJSON(w, http.StatusOK, d)
}
func (a *API) handlePatchWallet(w http.ResponseWriter, r *http.Request) {
	chain, address := chi.URLParam(r, "chain"), strings.ToLower(chi.URLParam(r, "address"))
	var p walletPatch
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer tx.Rollback(r.Context())
	var oldName, oldStatus string
	if err = tx.QueryRow(r.Context(), `SELECT entity_name,status FROM wallet_registry WHERE chain=$1 AND address=$2 FOR UPDATE`, chain, address).Scan(&oldName, &oldStatus); err != nil {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	name := oldName
	if p.EntityName != nil {
		name = *p.EntityName
	}
	status := oldStatus
	if p.Status != nil {
		status = *p.Status
	}
	typ := "unknown"
	if p.EntityType != nil {
		typ = *p.EntityType
	}
	notes := ""
	if p.Notes != nil {
		notes = *p.Notes
	}
	source := ""
	if p.VerificationSourceURL != nil {
		source = *p.VerificationSourceURL
	}
	balance := -1.0
	if p.KnownBalanceUSD != nil {
		balance = nonNegative(*p.KnownBalanceUSD)
	}
	_, err = tx.Exec(r.Context(), `UPDATE wallet_registry SET entity_id=COALESCE(NULLIF($8,'')::uuid,entity_id),entity_name=$3,entity_type=CASE WHEN $4='' THEN entity_type ELSE $4 END,status=$5,notes=CASE WHEN $6='' THEN notes ELSE $6 END,verification_source_url=CASE WHEN $7='' THEN verification_source_url ELSE $7 END,known_balance_usd=CASE WHEN $9 < 0 THEN known_balance_usd ELSE $9 END,label_source='admin',updated_at=now() WHERE chain=$1 AND address=$2`, chain, address, name, typ, status, notes, source, nullableString(p.EntityID), balance)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	_, _ = tx.Exec(r.Context(), `INSERT INTO wallet_label_history(chain,address,old_entity_name,new_entity_name,old_status,new_status,source_url,changed_at) VALUES($1,$2,$3,$4,$5,$6,$7,now())`, chain, address, oldName, name, oldStatus, status, source)
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	a.handleGetWallet(w, r)
}
func explorerAddress(chain, address string) string {
	switch strings.ToLower(chain) {
	case "ethereum":
		return "https://etherscan.io/address/" + address
	case "bitcoin":
		return "https://mempool.space/address/" + address
	case "solana":
		return "https://solscan.io/account/" + address
	default:
		return ""
	}
}
func nullableString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func nonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
