package workstore

// Connection bits describe the daemon transport that accepted Hello, not an OS
// liveness fact. Only its startup use case expires a previous daemon's bits;
// merely opening this store must not disconnect another running coordinator.
func (t *Tx) ListConnectedCarrierInstances() ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT DISTINCT instance_id FROM carrier_registrations WHERE connected=1 ORDER BY instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}
