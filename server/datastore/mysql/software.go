package mysql

import (
	"fmt"
	"strings"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

const (
	maxSoftwareNameLen    = 255
	maxSoftwareVersionLen = 255
	maxSoftwareSourceLen  = 64
)

func truncateString(str string, length int) string {
	if len(str) > length {
		return str[:length]
	}
	return str
}

func softwareToUniqueString(s fleet.Software) string {
	return strings.Join([]string{s.Name, s.Version, s.Source}, "\u0000")
}

func uniqueStringToSoftware(s string) fleet.Software {
	parts := strings.Split(s, "\u0000")
	return fleet.Software{
		Name:    truncateString(parts[0], maxSoftwareNameLen),
		Version: truncateString(parts[1], maxSoftwareVersionLen),
		Source:  truncateString(parts[2], maxSoftwareSourceLen),
	}
}

func softwareSliceToSet(softwares []fleet.Software) map[string]bool {
	result := make(map[string]bool)
	for _, s := range softwares {
		result[softwareToUniqueString(s)] = true
	}
	return result
}

func softwareSliceToIdMap(softwareSlice []fleet.Software) map[string]uint {
	result := make(map[string]uint)
	for _, s := range softwareSlice {
		result[softwareToUniqueString(s)] = s.ID
	}
	return result
}

func (d *Datastore) SaveHostSoftware(host *fleet.Host) error {
	if !host.HostSoftware.Modified {
		return nil
	}

	if err := d.withRetryTxx(func(tx *sqlx.Tx) error {
		if len(host.HostSoftware.Software) == 0 {
			// Clear join table for this host
			sql := "DELETE FROM host_software WHERE host_id = ?"
			if _, err := tx.Exec(sql, host.ID); err != nil {
				return errors.Wrap(err, "clear join table entries")
			}

			return nil
		}

		if err := d.applyChangesForNewSoftware(tx, host); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "save host software")
	}

	host.HostSoftware.Modified = false
	return nil
}

func nothingChanged(current []fleet.Software, incoming []fleet.Software) bool {
	if len(current) != len(incoming) {
		return false
	}

	currentBitmap := make(map[string]bool)
	for _, s := range current {
		currentBitmap[softwareToUniqueString(s)] = true
	}
	for _, s := range incoming {
		if _, ok := currentBitmap[softwareToUniqueString(s)]; !ok {
			return false
		}
	}

	return true
}

func (d *Datastore) applyChangesForNewSoftware(tx *sqlx.Tx, host *fleet.Host) error {
	storedCurrentSoftware, err := d.hostSoftwareFromHostID(tx, host.ID)
	if err != nil {
		return errors.Wrap(err, "loading current software for host")
	}

	if nothingChanged(storedCurrentSoftware, host.Software) {
		return nil
	}

	current := softwareSliceToIdMap(storedCurrentSoftware)
	incoming := softwareSliceToSet(host.Software)

	if err = d.deleteUninstalledHostSoftware(tx, host.ID, current, incoming); err != nil {
		return err
	}

	if err = d.insertNewInstalledHostSoftware(tx, host.ID, current, incoming); err != nil {
		return err
	}

	return nil
}

func (d *Datastore) deleteUninstalledHostSoftware(
	tx *sqlx.Tx,
	hostID uint,
	currentIdmap map[string]uint,
	incomingBitmap map[string]bool,
) error {
	var deletesHostSoftware []interface{}
	deletesHostSoftware = append(deletesHostSoftware, hostID)

	for currentKey := range currentIdmap {
		if _, ok := incomingBitmap[currentKey]; !ok {
			deletesHostSoftware = append(deletesHostSoftware, currentIdmap[currentKey])
			// TODO: delete from software if no host has it
		}
	}
	if len(deletesHostSoftware) <= 1 {
		return nil
	}
	sql := fmt.Sprintf(
		`DELETE FROM host_software WHERE host_id = ? AND software_id IN (%s)`,
		strings.TrimSuffix(strings.Repeat("?,", len(deletesHostSoftware)-1), ","),
	)
	if _, err := tx.Exec(sql, deletesHostSoftware...); err != nil {
		return errors.Wrap(err, "delete host software")
	}

	return nil
}

func (d *Datastore) getOrGenerateSoftwareId(tx *sqlx.Tx, s fleet.Software) (uint, error) {
	var existingId []int64
	if err := tx.Select(
		&existingId,
		`SELECT id FROM software WHERE name = ? and version = ? and source = ?`,
		s.Name, s.Version, s.Source,
	); err != nil {
		return 0, err
	}
	if len(existingId) > 0 {
		return uint(existingId[0]), nil
	}

	result, err := tx.Exec(
		`INSERT IGNORE INTO software (name, version, source) VALUES (?, ?, ?)`,
		s.Name, s.Version, s.Source,
	)
	if err != nil {
		return 0, errors.Wrap(err, "insert software")
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, errors.Wrap(err, "last id from software")
	}
	return uint(id), nil
}

func (d *Datastore) insertNewInstalledHostSoftware(
	tx *sqlx.Tx,
	hostID uint,
	currentIdmap map[string]uint,
	incomingBitmap map[string]bool,
) error {
	var insertsHostSoftware []interface{}
	for s := range incomingBitmap {
		if _, ok := currentIdmap[s]; !ok {
			id, err := d.getOrGenerateSoftwareId(tx, uniqueStringToSoftware(s))
			if err != nil {
				return err
			}
			insertsHostSoftware = append(insertsHostSoftware, hostID, id)
		}
	}
	if len(insertsHostSoftware) > 0 {
		values := strings.TrimSuffix(strings.Repeat("(?,?),", len(insertsHostSoftware)/2), ",")
		sql := fmt.Sprintf(`INSERT INTO host_software (host_id, software_id) VALUES %s`, values)
		if _, err := tx.Exec(sql, insertsHostSoftware...); err != nil {
			return errors.Wrap(err, "insert host software")
		}
	}

	return nil
}

func (d *Datastore) hostSoftwareFromHostID(tx *sqlx.Tx, id uint) ([]fleet.Software, error) {
	selectFunc := d.db.Select
	if tx != nil {
		selectFunc = tx.Select
	}
	sql := `
		SELECT * FROM software
		WHERE id IN
			(SELECT software_id FROM host_software WHERE host_id = ?)
	`
	var result []fleet.Software
	if err := selectFunc(&result, sql, id); err != nil {
		return nil, errors.Wrap(err, "load host software")
	}
	return result, nil
}

func (d *Datastore) LoadHostSoftware(host *fleet.Host) error {
	host.HostSoftware = fleet.HostSoftware{Modified: false}
	software, err := d.hostSoftwareFromHostID(nil, host.ID)
	if err != nil {
		return err
	}
	host.Software = software
	return nil
}
