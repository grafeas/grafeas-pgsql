// Copyright 2017 The Grafeas Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/fernet/fernet-go"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	"github.com/grafeas/grafeas/go/config"
	"github.com/grafeas/grafeas/go/name"
	"github.com/grafeas/grafeas/go/v1beta1/storage"
	pb "github.com/grafeas/grafeas/proto/v1beta1/grafeas_go_proto"
	prpb "github.com/grafeas/grafeas/proto/v1beta1/project_go_proto"
	"github.com/lib/pq"
	"golang.org/x/net/context"
	fieldmaskpb "google.golang.org/genproto/protobuf/field_mask"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config is the configuration for PostgreSQL store.
// json tags are required because
// config.ConvertGenericConfigToSpecificType internally uses json package.
type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// DBName has to alrady exist and can be accessed by User.
	DBName   string `json:"db_name"`
	User     string `json:"user"`
	Password string `json:"password"`
	// Valid sslmodes: disable, allow, prefer, require, verify-ca, verify-full.
	// See https://www.postgresql.org/docs/current/static/libpq-connect.html for details
	SSLMode     string `json:"ssl_mode"`
	SSLRootCert string `json:"ssl_root_cert"`
	// PaginationKey is a 32-bit URL-safe base64 key used to encrypt pagination tokens.
	// If one is not provided, it will be generated.
	// Multiple grafeas instances in the same cluster need the same value,
	// and the reason follows:
	// A client sends all requests to a unified load balancer,
	// but it can contain multiple Grafeas instances.
	// Consequently, if they do not share the same pagination key,
	// the encrypted page returned by one instance cannot be successfully decrypted by another instance.
	// As a result, if requests are routed to different Grafeas instances, pagination will be broken.
	PaginationKey string `json:"pagination_key"`
}

// PgSQLStore provides functionalities to use PostgreSQL DB as a data store.
type PgSQLStore struct {
	*sql.DB
	paginationKey string
}

// StorageTypeProvider creates and initializes a new grafeas v1beta1 storage compatible PgSQL store based on the specified config.
func StorageTypeProvider(_ string, ci *config.StorageConfiguration) (*storage.Storage, error) {
	var c Config
	err := config.ConvertGenericConfigToSpecificType(ci, &c)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to PostgreSQL-specific config, err: %v", err)
	}

	s, err := NewPgSQLStore(&c)
	if err != nil {
		return nil, err
	}

	return &storage.Storage{
		Ps: s,
		Gs: s,
	}, nil
}

// NewPgSQLStore creates a new PgSQL store based on the passed-in config.
func NewPgSQLStore(config *Config) (*PgSQLStore, error) {
	return NewStoreWithCustomConnector(newDSNConnector(*config), config.PaginationKey)
}

// dsnConnector references the implementation of sql.dsnConnector.
type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

// newDSNConnector returns a connector which
// simply parses the passed-in config into a DSN during initialization and reuses it forever.
func newDSNConnector(conf Config) *dsnConnector {
	connector := &dsnConnector{
		dsn:    assembleDSN(conf),
		driver: &pq.Driver{},
	}
	return connector
}

func assembleDSN(c Config) string {
	dsn := fmt.Sprintf("host=%s dbname=%s user=%s password=%s sslmode=%s",
		c.Host, c.DBName, c.User, c.Password, c.SSLMode,
	)
	if c.SSLRootCert != "" {
		dsn = fmt.Sprintf("%s sslrootcert=%s", dsn, c.SSLRootCert)
	}
	return dsn
}

func (c *dsnConnector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn)
}

func (c *dsnConnector) Driver() driver.Driver {
	return c.driver
}

// NewStoreWithCustomConnector creates a new PgSQL store using the custom connector.
func NewStoreWithCustomConnector(connector driver.Connector, paginationKey string) (*PgSQLStore, error) {
	if paginationKey == "" {
		log.Println("pagination key is empty, generating...")
		var key fernet.Key
		if err := key.Generate(); err != nil {
			return nil, fmt.Errorf("failed to generate pagination key, %s", err)
		}
		paginationKey = key.Encode()
	} else {
		// Validate pagination key
		_, err := fernet.DecodeKey(paginationKey)
		if err != nil {
			return nil, errors.New("invalid pagination key; must be 256-bit URL-safe base64")
		}
	}
	db := sql.OpenDB(connector)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping the database server, err: %v", err)
	}
	if _, err := db.Exec(createTables); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables, err: %v", err)
	}
	return &PgSQLStore{
		DB:            db,
		paginationKey: paginationKey,
	}, nil
}

// CreateProject adds the specified project to the store
func (pg *PgSQLStore) CreateProject(ctx context.Context, pID string, p *prpb.Project) (*prpb.Project, error) {
	_, err := pg.DB.ExecContext(ctx, insertProject, name.FormatProject(pID))
	if err, ok := err.(*pq.Error); ok {
		// Check for unique_violation
		if err.Code == "23505" {
			return nil, status.Errorf(codes.AlreadyExists, "Project with name %q already exists", pID)
		}
		log.Println("Failed to insert Project in database", err)
		return nil, status.Error(codes.Internal, "Failed to insert Project in database")
	}
	return p, nil
}

// DeleteProject deletes the project with the given pID from the store
func (pg *PgSQLStore) DeleteProject(ctx context.Context, pID string) error {
	pName := name.FormatProject(pID)
	result, err := pg.DB.ExecContext(ctx, deleteProject, pName)
	if err != nil {
		return status.Error(codes.Internal, "Failed to delete Project from database")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return status.Error(codes.Internal, "Failed to delete Project from database")
	}
	if count == 0 {
		return status.Errorf(codes.NotFound, "Project with name %q does not Exist", pName)
	}
	return nil
}

// GetProject returns the project with the given pID from the store
func (pg *PgSQLStore) GetProject(ctx context.Context, pID string) (*prpb.Project, error) {
	pName := name.FormatProject(pID)
	var exists bool
	err := pg.DB.QueryRowContext(ctx, projectExists, pName).Scan(&exists)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to query Project from database")
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project with name %q does not Exist", pName)
	}
	return &prpb.Project{Name: pName}, nil
}

// ListProjects returns up to pageSize number of projects beginning at pageToken (or from
// start if pageToken is the empty string).
func (pg *PgSQLStore) ListProjects(ctx context.Context, filter string, pageSize int, pageToken string) ([]*prpb.Project, string, error) {
	var filterQuery string
	if filter != "" {
		var fs FilterSQL
		filterQuery = " AND " + fs.ParseFilter(filter)
	}
	query := fmt.Sprintf(listProjects, filterQuery)
	id := decryptInt64(pageToken, pg.paginationKey, 0)
	rows, err := pg.DB.QueryContext(ctx, query, id, pageSize)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to list Projects from database")
	}
	var projects []*prpb.Project
	var lastID int64
	for rows.Next() {
		var name string
		err := rows.Scan(&lastID, &name)
		if err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to scan Project row")
		}
		projects = append(projects, &prpb.Project{Name: name})
	}
	if len(projects) == 0 {
		return projects, "", nil
	}
	maxQuery := projectsMaxID
	if filterQuery != "" {
		maxQuery = fmt.Sprintf("%s WHERE %s", maxQuery, filterQuery)
	}
	maxID, err := pg.max(ctx, maxQuery)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to query max project id from database")
	}
	if lastID >= maxID {
		return projects, "", nil
	}
	encryptedPage, err := encryptInt64(lastID, pg.paginationKey)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to paginate projects")
	}
	return projects, encryptedPage, nil
}

// CreateOccurrence adds the specified occurrence
func (pg *PgSQLStore) CreateOccurrence(ctx context.Context, pID, uID string, o *pb.Occurrence) (*pb.Occurrence, error) {
	o = proto.Clone(o).(*pb.Occurrence)
	o.CreateTime = ptypes.TimestampNow()

	var id string
	if nr, err := uuid.NewRandom(); err != nil {
		return nil, status.Error(codes.Internal, "Failed to generate UUID")
	} else {
		id = nr.String()
	}
	o.Name = fmt.Sprintf("projects/%s/occurrences/%s", pID, id)

	nPID, nID, err := name.ParseNote(o.NoteName)
	if err != nil {
		log.Printf("Invalid note name: %v", o.NoteName)
		return nil, status.Error(codes.InvalidArgument, "Invalid note name")
	}
	_, err = pg.DB.ExecContext(ctx, insertOccurrence, pID, id, nPID, nID, proto.MarshalTextString(o))
	if err, ok := err.(*pq.Error); ok {
		// Check for unique_violation
		if err.Code == "23505" {
			return nil, status.Errorf(codes.AlreadyExists, "Occurrence with name %q already exists", o.Name)
		}
		log.Println("Failed to insert Occurrence in database", err)
		return nil, status.Error(codes.Internal, "Failed to insert Occurrence in database")
	}
	return o, nil
}

// BatchCreateOccurrences batch creates the specified occurrences in PostreSQL.
func (pg *PgSQLStore) BatchCreateOccurrences(ctx context.Context, pID string, uID string, occs []*pb.Occurrence) ([]*pb.Occurrence, []error) {
	clonedOccs := []*pb.Occurrence{}
	for _, o := range occs {
		clonedOccs = append(clonedOccs, proto.Clone(o).(*pb.Occurrence))
	}
	occs = clonedOccs

	errs := []error{}
	created := []*pb.Occurrence{}
	for _, o := range occs {
		occ, err := pg.CreateOccurrence(ctx, pID, uID, o)
		if err != nil {
			// Occurrence already exists, skipping.
			continue
		} else {
			created = append(created, occ)
		}
	}

	return created, errs
}

// DeleteOccurrence deletes the occurrence with the given pID and oID
func (pg *PgSQLStore) DeleteOccurrence(ctx context.Context, pID, oID string) error {
	result, err := pg.DB.ExecContext(ctx, deleteOccurrence, pID, oID)
	if err != nil {
		return status.Error(codes.Internal, "Failed to delete Occurrence from database")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return status.Error(codes.Internal, "Failed to delete Occurrence from database")
	}
	if count == 0 {
		return status.Errorf(codes.NotFound, "Occurrence with name %q/%q does not Exist", pID, oID)
	}
	return nil
}

// UpdateOccurrence updates the existing occurrence with the given projectID and occurrenceID
func (pg *PgSQLStore) UpdateOccurrence(ctx context.Context, pID, oID string, o *pb.Occurrence, mask *fieldmaskpb.FieldMask) (*pb.Occurrence, error) {
	o = proto.Clone(o).(*pb.Occurrence)
	// TODO(#312): implement the update operation
	o.UpdateTime = ptypes.TimestampNow()

	result, err := pg.DB.ExecContext(ctx, updateOccurrence, proto.MarshalTextString(o), pID, oID)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to update Occurrence")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to update Occurrence")
	}
	if count == 0 {
		return nil, status.Errorf(codes.NotFound, "Occurrence with name %q/%q does not Exist", pID, oID)
	}
	return o, nil
}

// GetOccurrence returns the occurrence with pID and oID
func (pg *PgSQLStore) GetOccurrence(ctx context.Context, pID, oID string) (*pb.Occurrence, error) {
	var data string
	err := pg.DB.QueryRowContext(ctx, searchOccurrence, pID, oID).Scan(&data)
	switch {
	case err == sql.ErrNoRows:
		return nil, status.Errorf(codes.NotFound, "Occurrence with name %q/%q does not Exist", pID, oID)
	case err != nil:
		return nil, status.Error(codes.Internal, "Failed to query Occurrence from database")
	}
	var o pb.Occurrence
	if err = proto.UnmarshalText(data, &o); err != nil {
		return nil, status.Error(codes.Internal, "Failed to unmarshal Occurrence from database")
	}
	// Set the output-only field before returning
	o.Name = name.FormatOccurrence(pID, oID)
	return &o, nil
}

// ListOccurrences returns up to pageSize number of occurrences for this project beginning
// at pageToken, or from start if pageToken is the empty string.
func (pg *PgSQLStore) ListOccurrences(ctx context.Context, pID, filter, pageToken string, pageSize int32) ([]*pb.Occurrence, string, error) {
	var filterQuery string
	if filter != "" {
		var fs FilterSQL
		filterQuery = " AND " + fs.ParseFilter(filter)
	}

	query := fmt.Sprintf(listOccurrences, filterQuery)
	id := decryptInt64(pageToken, pg.paginationKey, 0)
	rows, err := pg.DB.QueryContext(ctx, query, pID, id, pageSize)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to list Occurrences from database")
	}

	var os []*pb.Occurrence
	var lastID int64
	for rows.Next() {
		var data string
		err := rows.Scan(&lastID, &data)
		if err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to scan Occurrences row")
		}
		var o pb.Occurrence
		if err = proto.UnmarshalText(data, &o); err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to unmarshal Occurrence from database")
		}
		os = append(os, &o)
	}
	if len(os) == 0 {
		return os, "", nil
	}
	maxQuery := fmt.Sprintf(occurrenceMaxID, filterQuery)
	maxID, err := pg.max(ctx, maxQuery, pID)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to query max occurrence id from database")
	}
	if lastID >= maxID {
		return os, "", nil
	}
	encryptedPage, err := encryptInt64(lastID, pg.paginationKey)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to paginate occurrences")
	}
	return os, encryptedPage, nil
}

// CreateNote adds the specified note
func (pg *PgSQLStore) CreateNote(ctx context.Context, pID, nID, uID string, n *pb.Note) (*pb.Note, error) {
	n = proto.Clone(n).(*pb.Note)
	nName := name.FormatNote(pID, nID)
	n.Name = nName
	n.CreateTime = ptypes.TimestampNow()

	_, err := pg.DB.ExecContext(ctx, insertNote, pID, nID, proto.MarshalTextString(n))
	if err, ok := err.(*pq.Error); ok {
		// Check for unique_violation
		if err.Code == "23505" {
			return nil, status.Errorf(codes.AlreadyExists, "Note with name %q already exists", n.Name)
		}
		log.Println("Failed to insert Note in database", err)
		return nil, status.Error(codes.Internal, "Failed to insert Note in database")
	}
	return n, nil
}

// BatchCreateNotes batch creates the specified notes in memstore.
func (pg *PgSQLStore) BatchCreateNotes(ctx context.Context, pID, uID string, notes map[string]*pb.Note) ([]*pb.Note, []error) {
	clonedNotes := map[string]*pb.Note{}
	for nID, n := range notes {
		clonedNotes[nID] = proto.Clone(n).(*pb.Note)
	}
	notes = clonedNotes

	errs := []error{}
	created := []*pb.Note{}
	for nID, n := range notes {
		note, err := pg.CreateNote(ctx, pID, nID, uID, n)
		if err != nil {
			// Note already exists, skipping.
			continue
		} else {
			created = append(created, note)
		}
	}
	return created, errs
}

// DeleteNote deletes the note with the given pID and nID
func (pg *PgSQLStore) DeleteNote(ctx context.Context, pID, nID string) error {
	result, err := pg.DB.ExecContext(ctx, deleteNote, pID, nID)
	if err != nil {
		return status.Error(codes.Internal, "Failed to delete Note from database")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return status.Error(codes.Internal, "Failed to delete Note from database")
	}
	if count == 0 {
		return status.Errorf(codes.NotFound, "Note with name %q/%q does not Exist", pID, nID)
	}
	return nil
}

// UpdateNote updates the existing note with the given pID and nID
func (pg *PgSQLStore) UpdateNote(ctx context.Context, pID, nID string, n *pb.Note, mask *fieldmaskpb.FieldMask) (*pb.Note, error) {
	n = proto.Clone(n).(*pb.Note)
	nName := name.FormatNote(pID, nID)
	n.Name = nName
	// TODO(#312): implement the update operation
	n.UpdateTime = ptypes.TimestampNow()

	result, err := pg.DB.ExecContext(ctx, updateNote, proto.MarshalTextString(n), pID, nID)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to update Note")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to update Note")
	}
	if count == 0 {
		return nil, status.Errorf(codes.NotFound, "Note with name %q/%q does not Exist", pID, nID)
	}
	return n, nil
}

// GetNote returns the note with project (pID) and note ID (nID)
func (pg *PgSQLStore) GetNote(ctx context.Context, pID, nID string) (*pb.Note, error) {
	var data string
	err := pg.DB.QueryRowContext(ctx, searchNote, pID, nID).Scan(&data)
	switch {
	case err == sql.ErrNoRows:
		return nil, status.Errorf(codes.NotFound, "Note with name %q/%q does not Exist", pID, nID)
	case err != nil:
		return nil, status.Error(codes.Internal, "Failed to query Note from database")
	}
	var note pb.Note
	if err = proto.UnmarshalText(data, &note); err != nil {
		return nil, status.Error(codes.Internal, "Failed to unmarshal Note from database")
	}
	// Set the output-only field before returning
	note.Name = name.FormatNote(pID, nID)
	return &note, nil
}

// GetOccurrenceNote gets the note for the specified occurrence from PostgreSQL.
func (pg *PgSQLStore) GetOccurrenceNote(ctx context.Context, pID, oID string) (*pb.Note, error) {
	o, err := pg.GetOccurrence(ctx, pID, oID)
	if err != nil {
		return nil, err
	}
	nPID, nID, err := name.ParseNote(o.NoteName)
	if err != nil {
		log.Printf("Error parsing name: %v", o.NoteName)
		return nil, status.Error(codes.InvalidArgument, "Invalid Note name")
	}
	n, err := pg.GetNote(ctx, nPID, nID)
	if err != nil {
		return nil, err
	}
	// Set the output-only field before returning
	n.Name = name.FormatNote(nPID, nID)
	return n, nil
}

// ListNotes returns up to pageSize number of notes for this project (pID) beginning
// at pageToken (or from start if pageToken is the empty string).
func (pg *PgSQLStore) ListNotes(ctx context.Context, pID, filter, pageToken string, pageSize int32) ([]*pb.Note, string, error) {
	var filterQuery string
	if filter != "" {
		var fs FilterSQL
		filterQuery = " AND " + fs.ParseFilter(filter)
	}

	query := fmt.Sprintf(listNotes, filterQuery)
	id := decryptInt64(pageToken, pg.paginationKey, 0)
	rows, err := pg.DB.QueryContext(ctx, query, pID, id, pageSize)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to list Notes from database")
	}

	var ns []*pb.Note
	var lastID int64
	for rows.Next() {
		var data string
		err := rows.Scan(&lastID, &data)
		if err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to scan Notes row")
		}
		var n pb.Note
		if err = proto.UnmarshalText(data, &n); err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to unmarshal Note from database")
		}
		ns = append(ns, &n)
	}
	if len(ns) == 0 {
		return ns, "", nil
	}
	maxQuery := fmt.Sprintf(notesMaxID, filterQuery)
	maxID, err := pg.max(ctx, maxQuery, pID)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to query max note id from database")
	}
	if lastID >= maxID {
		return ns, "", nil
	}
	encryptedPage, err := encryptInt64(lastID, pg.paginationKey)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to paginate notes")
	}
	return ns, encryptedPage, nil
}

// ListNoteOccurrences returns up to pageSize number of occcurrences on the particular note (nID)
// for this project (pID) projects beginning at pageToken (or from start if pageToken is the empty string).
// TODO: implement query filter for NoteOccurrences.
// ListNoteOccurrences is not used by grafeas-client currently.
func (pg *PgSQLStore) ListNoteOccurrences(ctx context.Context, pID, nID, filter, pageToken string, pageSize int32) ([]*pb.Occurrence, string, error) {
	// Verify that note exists
	if _, err := pg.GetNote(ctx, pID, nID); err != nil {
		return nil, "", err
	}
	id := decryptInt64(pageToken, pg.paginationKey, 0)
	rows, err := pg.DB.QueryContext(ctx, listNoteOccurrences, pID, nID, id, pageSize)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to list Occurrences from database")
	}

	var os []*pb.Occurrence
	var lastID int64
	for rows.Next() {
		var data string
		err := rows.Scan(&lastID, &data)
		if err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to scan Occurrences row")
		}
		var o pb.Occurrence
		if err = proto.UnmarshalText(data, &o); err != nil {
			return nil, "", status.Error(codes.Internal, "Failed to unmarshal Occurrence from database")
		}
		os = append(os, &o)
	}
	if len(os) == 0 {
		return os, "", nil
	}
	maxID, err := pg.max(ctx, NoteOccurrencesMaxID, pID, nID)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to query max NoteOccurrences from database")
	}
	if lastID >= maxID {
		return os, "", nil
	}
	encryptedPage, err := encryptInt64(lastID, pg.paginationKey)
	if err != nil {
		return nil, "", status.Error(codes.Internal, "Failed to paginate note occurrences")
	}
	return os, encryptedPage, nil
}

// GetVulnerabilityOccurrencesSummary gets a summary of vulnerability occurrences from storage.
func (pg *PgSQLStore) GetVulnerabilityOccurrencesSummary(ctx context.Context, projectID, filter string) (*pb.VulnerabilityOccurrencesSummary, error) {
	return &pb.VulnerabilityOccurrencesSummary{}, nil
}

// max returns the max ID of entries for the specified query (assuming SELECT(*) is used)
func (pg *PgSQLStore) max(ctx context.Context, query string, args ...interface{}) (int64, error) {
	row := pg.DB.QueryRowContext(ctx, query, args...)
	var count int64
	err := row.Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, err
}

// encryptInt64 encrypts int64 using provided key
func encryptInt64(v int64, key string) (string, error) {
	k, err := fernet.DecodeKey(key)
	if err != nil {
		return "", err
	}
	bytes, err := fernet.EncryptAndSign([]byte(strconv.FormatInt(v, 10)), k)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// decryptInt64 decrypts encrypted int64 using provided key. Returns defaultValue if decryption fails.
func decryptInt64(encrypted string, key string, defaultValue int64) int64 {
	k, err := fernet.DecodeKey(key)
	if err != nil {
		return defaultValue
	}
	bytes := fernet.VerifyAndDecrypt([]byte(encrypted), time.Hour, []*fernet.Key{k})
	if bytes == nil {
		return defaultValue
	}
	decryptedValue, err := strconv.ParseInt(string(bytes), 10, 64)
	if err != nil {
		return defaultValue
	}
	return decryptedValue
}
