// Copyright 2019 The Grafeas Authors. All rights reserved.
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
	"os"
	"testing"

	"github.com/grafeas/grafeas/go/config"
	grafeas "github.com/grafeas/grafeas/go/v1beta1/api"
	"github.com/grafeas/grafeas/go/v1beta1/project"
	"github.com/grafeas/grafeas/go/v1beta1/storage"
)

type testPgHelper struct {
	pgDataPath string
	pgBinPath  string
	startedPg  bool
	pgConfig   *config.PgSQLConfig
}

var (
	//Unfortunately, not a good way to pass this information around to tests except via a globally scoped var
	pgsqlstoreTestPgConfig *testPgHelper
)

func dropDatabase(t *testing.T, config *config.PgSQLConfig) {
	t.Helper()
	// Open database
	source := storage.CreateSourceString(config.User, config.Password, config.Host, "postgres", config.SSLMode)
	db, err := sql.Open("postgres", source)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	// Kill opened connection
	if _, err := db.Exec(`
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1`, config.DbName); err != nil {
		t.Fatalf("Failed to drop database: %v", err)
	}
	// Drop database
	if _, err := db.Exec("DROP DATABASE " + config.DbName); err != nil {
		t.Fatalf("Failed to drop database: %v", err)
	}
}

func TestBetaPgSQLStore(t *testing.T) {
	createPgSQLStore := func(t *testing.T) (grafeas.Storage, project.Storage, func()) {
		t.Helper()
		config := &config.PgSQLConfig{
			Host:          pgsqlstoreTestPgConfig.pgConfig.Host,
			DbName:        "test_db",
			User:          pgsqlstoreTestPgConfig.pgConfig.User,
			Password:      pgsqlstoreTestPgConfig.pgConfig.Password,
			SSLMode:       pgsqlstoreTestPgConfig.pgConfig.SSLMode,
			PaginationKey: "XxoPtCUzrUv4JV5dS+yQ+MdW7yLEJnRMwigVY/bpgtQ=",
		}
		pg, err := NewPgSQLStore(config)
		if err != nil {
			t.Errorf("Error creating PgSQLStore, %s", err)
		}
		var g grafeas.Storage = pg
		var gp project.Storage = pg
		return g, gp, func() { dropDatabase(t, config); pg.Close() }
	}

	storage.DoTestStorage(t, createPgSQLStore)
}

func TestPgSQLStoreWithUserAsEnv(t *testing.T) {
	createPgSQLStore := func(t *testing.T) (grafeas.Storage, project.Storage, func()) {
		t.Helper()
		config := &config.PgSQLConfig{
			Host:          "127.0.0.1:5432",
			DbName:        "test_db",
			User:          "",
			Password:      "",
			SSLMode:       "disable",
			PaginationKey: "XxoPtCUzrUv4JV5dS+yQ+MdW7yLEJnRMwigVY/bpgtQ=",
		}
		_ = os.Setenv("PGUSER", "postgres")
		_ = os.Setenv("PGPASSWORD", "password")
		pg, err := storage.NewPgSQLStore(config)
		if err != nil {
			t.Errorf("Error creating PgSQLStore: %v", err)
		}
		var g grafeas.Storage = pg
		var gp project.Storage = pg
		return g, gp, func() { dropDatabase(t, config); pg.Close() }
	}
	storage.DoTestStorage(t, createPgSQLStore)
}
