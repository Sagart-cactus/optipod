package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/optipod/optipod/internal/consoleversion"
)

type AgentHealth struct {
	Version            string     `json:"version"`
	KubernetesVersion  string     `json:"kubernetesVersion"`
	QueueBytes         int64      `json:"queueBytes"`
	QueueLimit         int64      `json:"queueLimit"`
	LastCollection     *time.Time `json:"lastCollection,omitempty"`
	LastUpload         *time.Time `json:"lastUpload,omitempty"`
	CertificateExpires *time.Time `json:"certificateExpires,omitempty"`
}

type ClusterHealth struct {
	ID                string      `json:"id"`
	Disconnected      bool        `json:"disconnected"`
	InventoryReceived *time.Time  `json:"inventoryReceived,omitempty"`
	HeartbeatReceived *time.Time  `json:"heartbeatReceived,omitempty"`
	CredentialExpires *time.Time  `json:"credentialExpires,omitempty"`
	Agent             AgentHealth `json:"agent"`
	Warnings          []string    `json:"warnings"`
}

type RecoveryCheck struct {
	VerifiedAt    time.Time       `json:"verifiedAt"`
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schemaVersion"`
	Details       json.RawMessage `json:"details"`
}

type HealthReport struct {
	Database string          `json:"database"`
	Version  string          `json:"version"`
	Clusters []ClusterHealth `json:"clusters"`
	Recovery []RecoveryCheck `json:"recovery"`
}

func (s *Store) Health(ctx context.Context) (*HealthReport, error) {
	result := &HealthReport{Database: "available", Version: consoleversion.Version, Clusters: []ClusterHealth{}, Recovery: []RecoveryCheck{}}
	rows, err := s.Pool.Query(ctx, `SELECT ids.id,COALESCE(r.disconnected,false),c.received_at,h.received_at,COALESCE(h.data,'{}'::jsonb),
	 (SELECT max(expires_at) FROM op_credentials WHERE cluster_id=ids.id AND kind='runtime' AND revoked_at IS NULL)
	 FROM (SELECT id FROM op_registrations UNION SELECT id FROM op_clusters) ids
	 LEFT JOIN op_registrations r ON r.id=ids.id LEFT JOIN op_clusters c ON c.id=ids.id LEFT JOIN op_agent_health h ON h.cluster_id=ids.id ORDER BY ids.id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item ClusterHealth
		if err = rows.Scan(&item.ID, &item.Disconnected, &item.InventoryReceived, &item.HeartbeatReceived, &item.Agent, &item.CredentialExpires); err != nil {
			rows.Close()
			return nil, err
		}
		item.Warnings = healthWarnings(item)
		result.Clusters = append(result.Clusters, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT verified_at,kind,schema_version,details FROM op_recovery_checks ORDER BY verified_at DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var check RecoveryCheck
		if err = rows.Scan(&check.VerifiedAt, &check.Kind, &check.SchemaVersion, &check.Details); err != nil {
			return nil, err
		}
		result.Recovery = append(result.Recovery, check)
	}
	return result, rows.Err()
}

func healthWarnings(c ClusterHealth) []string {
	warnings := []string{}
	if c.Disconnected {
		return []string{"Telemetry disconnected; optimizer remains independent"}
	}
	if c.InventoryReceived == nil {
		warnings = append(warnings, "Awaiting first complete inventory")
	} else if time.Since(*c.InventoryReceived) > 3*time.Minute {
		warnings = append(warnings, "Inventory is stale; check agent connectivity and queue")
	}
	if c.HeartbeatReceived == nil {
		warnings = append(warnings, "No agent health heartbeat received")
	} else if time.Since(*c.HeartbeatReceived) > 3*time.Minute {
		warnings = append(warnings, "Agent heartbeat is stale")
	}
	if c.Agent.QueueLimit > 0 && c.Agent.QueueBytes*100/c.Agent.QueueLimit >= 80 {
		warnings = append(warnings, "Queue above 80%; restore delivery before capacity is exhausted")
	}
	if !consoleversion.Compatible(c.Agent.Version) {
		warnings = append(warnings, "Agent version unknown or incompatible; use the matching console chart version")
	}
	if c.CredentialExpires != nil && time.Until(*c.CredentialExpires) < 7*24*time.Hour {
		warnings = append(warnings, "Runtime credential expires within seven days; rotate or reconnect")
	}
	if c.Agent.CertificateExpires != nil && time.Until(*c.Agent.CertificateExpires) < 8*time.Hour {
		warnings = append(warnings, "Client certificate renewal is due")
	}
	return warnings
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodPost) {
		return
	}
	a, err := s.authenticatedAgent(r)
	if err != nil {
		s.unavailable(w, err)
		return
	}
	if a == nil {
		http.Error(w, "agent authentication required", http.StatusUnauthorized)
		return
	}
	var data AgentHealth
	if !decodeInput(w, r, &data) {
		return
	}
	if len(data.Version) > 80 || len(data.KubernetesVersion) > 80 || data.QueueBytes < 0 || data.QueueLimit < 0 || data.QueueLimit > 1<<40 || data.QueueBytes > 1<<40 {
		s.onboardingError(w, ErrInvalid)
		return
	}
	b, _ := json.Marshal(data)
	_, err = s.Store.Pool.Exec(r.Context(), `INSERT INTO op_agent_health(cluster_id,data) VALUES($1,$2) ON CONFLICT(cluster_id) DO UPDATE SET received_at=now(),data=$2`, a.ID, b)
	s.adminResult(w, map[string]bool{"received": true}, err)
}

type Notification struct {
	ID             int64      `json:"id"`
	ClusterID      string     `json:"clusterID"`
	Kind           string     `json:"kind"`
	Message        string     `json:"message"`
	CreatedAt      time.Time  `json:"createdAt"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt,omitempty"`
}

func (s *Store) Notifications(ctx context.Context) ([]Notification, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,cluster_id,kind,message,created_at,acknowledged_at FROM op_notifications ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Notification{}
	for rows.Next() {
		var n Notification
		if err = rows.Scan(&n.ID, &n.ClusterID, &n.Kind, &n.Message, &n.CreatedAt, &n.AcknowledgedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func (s *Store) MaintainAlerts(ctx context.Context) error {
	health, err := s.Health(ctx)
	if err != nil {
		return err
	}
	for _, cluster := range health.Clusters {
		if cluster.Disconnected {
			continue
		}
		for _, warning := range cluster.Warnings {
			key := cluster.ID + ":" + warning + ":" + time.Now().UTC().Format("2006-01-02")
			if _, err = s.Pool.Exec(ctx, `INSERT INTO op_notifications(event_key,cluster_id,kind,message) VALUES($1,$2,'health',$3) ON CONFLICT DO NOTHING`, key, cluster.ID, warning); err != nil {
				return err
			}
		}
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM op_notifications WHERE acknowledged_at < now()-interval '90 days'`)
	return err
}

func (s *Store) AcknowledgeNotification(ctx context.Context, actor string, id int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE op_notifications SET acknowledged_at=now() WHERE id=$1 AND acknowledged_at IS NULL`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		if err = audit(ctx, tx, actor, "notification.acknowledge", fmt.Sprint(id), map[string]int64{"id": id}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
