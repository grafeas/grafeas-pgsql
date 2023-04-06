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
	"fmt"
	"log"
	"strings"

	expr "github.com/grafeas/grafeas/cel"
	"github.com/grafeas/grafeas/go/filtering/common"
	"github.com/grafeas/grafeas/go/filtering/operators"
	"github.com/grafeas/grafeas/go/filtering/parser"
)

type FilterSQL struct {
	selects int
}

func (fs *FilterSQL) sqlFromCall(funcName string, args []*expr.Expr) string {
	var sqlOp string
	switch funcName {
	case operators.Equals:
		sqlOp = "="
	case operators.Greater:
		sqlOp = ">"
	case operators.GreaterEquals:
		sqlOp = ">="
	case operators.Less:
		sqlOp = "<"
	case operators.LessEquals:
		sqlOp = "<="
	case operators.NotEquals:
		sqlOp = "!="
	case operators.LogicalAnd:
		sqlOp = "AND"
	case operators.LogicalOr:
		sqlOp = "OR"
	case operators.Index:
		sqlOp = "["
	case operators.Has:
		sqlOp = "like"
	default:
		sqlOp = ""
	}
	var argNames []string
	for _, arg := range args {
		argNames = append(argNames, fs.makeSQL(arg))
	}
	if sqlOp == "[" {
		return fmt.Sprintf("%s[%s]", argNames[0], argNames[1])
	} else if sqlOp == "like" {
		return fmt.Sprintf("(%s %s %s)", argNames[0], sqlOp, formatLikeString(argNames[1]))
	} else if sqlOp != "" {
		return fmt.Sprintf("(%s %s %s)", argNames[0], sqlOp, argNames[1])
	}
	return fmt.Sprintf("%s(%s)", funcName, strings.Join(argNames, ", "))
}

func (fs *FilterSQL) sqlFromSelect(selectNode *expr.Expr_Select) string {
	operand := fs.makeSQL(selectNode.GetOperand())
	field := selectNode.GetField()
	return fmt.Sprintf("%s.%s", operand, field)
}

func (fs *FilterSQL) getConstantValue(constExpr *expr.Constant) string {
	switch constExpr.GetConstantKind().(type) {
	case *expr.Constant_Int64Value:
		return fmt.Sprintf("%d", constExpr.GetInt64Value())
	case *expr.Constant_Uint64Value:
		return fmt.Sprintf("%d", constExpr.GetUint64Value())
	case *expr.Constant_DoubleValue:
		return fmt.Sprintf("%f", constExpr.GetDoubleValue())
	case *expr.Constant_StringValue:
		return fmt.Sprintf("'%s'", constExpr.GetStringValue())
	}
	return "NO CONST"
}

func (fs *FilterSQL) makeSQL(node *expr.Expr) string {
	switch node.GetExprKind().(type) {
	case *expr.Expr_CallExpr:
		funcNode := *node.GetCallExpr()
		return fs.sqlFromCall(funcNode.Function, funcNode.Args)
	case *expr.Expr_SelectExpr:
		selectNode := *node.GetSelectExpr()
		fs.selects++
		retStr := fs.sqlFromSelect(&selectNode)
		fs.selects--
		if fs.selects == 0 {
			spl := strings.Split(retStr, ".")
			retVal := "data"
			sep := "->'"
			sep2 := "->>'"
			for i := 0; i < len(spl); i++ {
				if i != len(spl)-1 {
					retVal = retVal + sep + spl[i] + "'"
				} else {
					retVal = retVal + sep2 + spl[i] + "'"
				}
			}

			//return "data->'$." + ret_str + "'"
			//return "data->'" + ret_str + "'"
			return retVal
		}
		return retStr
	case *expr.Expr_IdentExpr:
		i_expr := *node.GetIdentExpr()
		// I'm not entirely sure this is the right thing here.
		// We'll see though.
		if fs.selects > 0 {
			return i_expr.Name
		}
		//return "data->'$." + i_expr.Name + "'"
		return "data->>'" + i_expr.Name + "'"
	case *expr.Expr_ConstExpr:
		c_expr := *node.GetConstExpr()
		return fs.getConstantValue(&c_expr)
	}

	return "NO SQL"

}

// ParseFilter parses the incoming filter and returns a formatted SQL query.
func (fs *FilterSQL) ParseFilter(filter string) string {
	s := common.NewStringSource(filter, "urlParam") // function
	result, err := parser.Parse(s)
	if err != nil {
		log.Println(err)
		return ""
	}
	sql := fs.makeSQL(result.Expr)
	return sql
}

func formatLikeString(argName string) string {
	return "'%" + strings.Split(argName, "'")[1] + "%'"
}