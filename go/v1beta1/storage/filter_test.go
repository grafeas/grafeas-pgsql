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
	"log"
	"testing"
)

func TestPgsqlFilterSql_ParseFilter(t *testing.T) {
	fs := FilterSQL{}
	tests := map[string]struct {
		filter string
		want   string
	}{
		"check if resource uri equal to either one of the values": {
			filter: `resource.uri="a.rpm" OR resource.uri="https://a.com/b/c/a.rpm"`,
			want:   `((data->'resource'->>'uri' = 'a.rpm') OR (data->'resource'->>'uri' = 'https://a.com/b/c/a.rpm'))`,
		},
		"greater than": {
			filter: `resource.min_value>10 AND resource.max_value<100`,
			want:   `((data->'resource'->>'min_value' > 10) AND (data->'resource'->>'max_value' < 100))`,
		},
	}
	for label, tt := range tests {
		label, tt := label, tt
		t.Run(label, func(t *testing.T) {
			got := fs.ParseFilter(tt.filter)
			log.Printf("got: %s", got)
			if got != tt.want {
				t.Fatalf("%s: want: %q got: %q", label, tt.want, got)
			}
		})
	}
}
