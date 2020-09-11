// Copyright 2020 Juan Pablo Tosso
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package operators

import(
	_"strings"
	"github.com/jptosso/coraza-waf/pkg/engine"
)

type BeginsWith struct{
	data string
	dlen int
}

func (o *BeginsWith) Init(data string){
	o.data = data
	o.dlen = len(data)
}

func (o *BeginsWith) Evaluate(tx *engine.Transaction, value string) bool{
	if len(value) < o.dlen{
		return false
	}
	return o.data == value[0:o.dlen]
	//return strings.HasPrefix(value, o.data)
}