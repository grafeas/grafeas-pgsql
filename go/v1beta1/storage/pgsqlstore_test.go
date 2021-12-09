package storage

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/grafeas/grafeas/go/name"
	prpb "github.com/grafeas/grafeas/proto/v1beta1/project_go_proto"
	"golang.org/x/net/context"
)

const (
	pid           = "pid"
	nid           = "nid"
	paginationKey = "nQi0NzMjerFtlMnbylnWzMrIlNCsuyzeq8LnBEkgxrk=" // go get -v github.com/fernet/fernet-go/cmd/fernet-keygen ; fernet-keygen
)

func genTestDataProjects() ([]*prpb.Project, []string, error) {
	var prjs []*prpb.Project
	var prjsData []string
	for i := 1; i <= 5; i++ {
		s := name.FormatProject(fmt.Sprintf("projects/p%d", i))
		p := &prpb.Project{
			Name: s,
		}
		prjs = append(prjs, p)
		prjsData = append(prjsData, string(s))
	}
	return prjs, prjsData, nil
}

func TestStore_ListProjects(t *testing.T) {
	projects, projectsData, err := genTestDataProjects()
	if err != nil {
		t.Fatalf("failed to genTestDataProjects, err: %v", err)
	}

	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tests := []struct {
		name            string
		getStore        func(t *testing.T) (*PgSQLStore, func())
		filter          string
		pageToken       string
		pageSize        int
		want            []*prpb.Project
		wantDecryptedID int64
		wantErr         bool
	}{
		{
			name: "happy path",
			getStore: func(t *testing.T) (*PgSQLStore, func()) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
				}

				rows := sqlmock.NewRows([]string{"id", "data"})
				for i, o := range projectsData {
					rows = rows.AddRow(i+1, o) // index id starts from 1
				}
				mock.ExpectQuery("SELECT id, name FROM projects").
					WillReturnRows(rows)
				mock.ExpectQuery(`SELECT MAX\(id\) FROM projects`).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(len(projectsData))))
				s := &PgSQLStore{DB: db}
				return s, func() { db.Close() }
			},
			want: projects,
		},
		{
			name: "pagination",
			getStore: func(t *testing.T) (*PgSQLStore, func()) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
				}

				rows := sqlmock.NewRows([]string{"id", "data"})
				for i := 0; i < 2; i++ {
					rows = rows.AddRow(i+1, projectsData[i]) // index id starts from 1
				}
				mock.ExpectQuery("SELECT id, name FROM projects").
					WillReturnRows(rows)
				mock.ExpectQuery(`SELECT MAX\(id\) FROM projects`).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(len(projectsData))))
				s := &PgSQLStore{DB: db, paginationKey: paginationKey}
				return s, func() { db.Close() }
			},
			want:            projects[0:2],
			wantDecryptedID: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, cancel := tt.getStore(t)
			defer cancel()
			got, nextToken, err := s.ListProjects(ctx, tt.filter, tt.pageSize, tt.pageToken)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListProjects() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ListProjects() got = %v, want %v", got, tt.want)
			}
			decryptedTokenID := decryptInt64(nextToken, s.paginationKey, 0)
			if decryptedTokenID != tt.wantDecryptedID {
				t.Errorf("ListProjects() got1 = %v, want %v", nextToken, tt.wantDecryptedID)
			}
		})
	}
}
