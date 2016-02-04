package exec

import (
	"database/sql/driver"

	u "github.com/araddon/gou"

	"github.com/araddon/qlbridge/datasource"
	"github.com/araddon/qlbridge/plan"
	"github.com/araddon/qlbridge/schema"
	"github.com/araddon/qlbridge/value"
	"github.com/araddon/qlbridge/vm"
)

// Projection Execution Task
type Projection struct {
	*TaskBase
	p *plan.Projection
}

// In Process projections are used when mapping multiple sources together
//  and additional columns such as those used in Where, GroupBy etc are used
//  even if they will not be used in Final projection
func NewProjection(ctx *plan.Context, p *plan.Projection) *Projection {
	if p.Final {
		return NewProjectionFinal(ctx, p)
	}
	return NewProjectionInProcess(ctx, p)
}

// In Process projections are used when mapping multiple sources together
//  and additional columns such as those used in Where, GroupBy etc are used
//  even if they will not be used in Final projection
func NewProjectionInProcess(ctx *plan.Context, p *plan.Projection) *Projection {
	s := &Projection{
		TaskBase: NewTaskBase(ctx),
		p:        p,
	}
	s.Handler = s.projectionEvaluator(p.Final)
	return s
}

// Final Projections project final select columns for result-writing
func NewProjectionFinal(ctx *plan.Context, p *plan.Projection) *Projection {
	s := &Projection{
		TaskBase: NewTaskBase(ctx),
		p:        p,
	}
	s.Handler = s.projectionEvaluator(p.Final)
	return s
}

// Create handler function for evaluation (ie, field selection from tuples)
func (m *Projection) projectionEvaluator(isFinal bool) MessageHandler {
	out := m.MessageOut()
	columns := m.p.Stmt.Columns
	colIndex := m.p.Stmt.ColIndexes()

	return func(ctx *plan.Context, msg schema.Message) bool {
		// defer func() {
		// 	if r := recover(); r != nil {
		// 		u.Errorf("crap, %v", r)
		// 	}
		// }()

		//u.Infof("got projection message: %T %#v", msg, msg.Body())
		var outMsg schema.Message
		switch mt := msg.(type) {
		case *datasource.SqlDriverMessageMap:
			// readContext := datasource.NewContextUrlValues(uv)
			// use our custom write context for example purposes
			row := make([]driver.Value, len(columns))
			//u.Debugf("about to project: %#v", mt)
			colCt := 0
			for i, col := range columns {
				//u.Debugf("col: idx:%v sidx: %v pidx:%v key:%v   %s", col.Index, col.SourceIndex, col.ParentIndex, col.Key(), col.Expr)

				if isFinal && col.ParentIndex < 0 {
					continue
				}

				if col.Guard != nil {
					ifColValue, ok := vm.Eval(mt, col.Guard)
					if !ok {
						u.Errorf("Could not evaluate if:   %v", col.Guard.String())
						//return fmt.Errorf("Could not evaluate if clause: %v", col.Guard.String())
					}
					//u.Debugf("if eval val:  %T:%v", ifColValue, ifColValue)
					switch ifColVal := ifColValue.(type) {
					case value.BoolValue:
						if ifColVal.Val() == false {
							//u.Debugf("Filtering out col")
							continue
						}
					}
				}
				if col.Star {
					starRow := mt.Row()
					newRow := make([]driver.Value, len(starRow)+len(colIndex))
					for curi := 0; curi < i; curi++ {
						newRow[curi] = row[curi]
					}
					row = newRow
					for _, v := range starRow {
						colCt += 1
						//writeContext.Put(&expr.Column{As: k}, nil, value.NewValue(v))
						row[i+colCt] = v
					}
				} else if col.Expr == nil {
					u.Warnf("wat?   nil col expr? %#v", col)
				} else {
					v, ok := vm.Eval(mt, col.Expr)
					if !ok {
						u.Warnf("failed eval key=%v  val=%#v expr:%s   mt:%#v", col.Key(), v, col.Expr, mt)
					} else if v == nil {
						//u.Debugf("%#v", col)
						//u.Debugf("evaled nil? key=%v  val=%v expr:%s", col.Key(), v, col.Expr.String())
						//writeContext.Put(col, mt, v)
						//u.Infof("mt: %T  mt %#v", mt, mt)
						row[i+colCt] = nil //v.Value()
					} else {
						//u.Debugf("evaled: key=%v  val=%v", col.Key(), v.Value())
						//writeContext.Put(col, mt, v)
						row[i+colCt] = v.Value()
					}
				}
			}
			//u.Infof("row: %#v", row)
			//u.Infof("row cols: %v", colIndex)
			outMsg = datasource.NewSqlDriverMessageMap(0, row, colIndex)
		/*
			case *datasource.ContextUrlValues:
				// readContext := datasource.NewContextUrlValues(uv)
				// use our custom write context for example purposes
				writeContext := datasource.NewContextSimple()
				outMsg = writeContext
				//u.Infof("about to project: colsct%v %#v", len(sql.Columns), outMsg)
				for _, col := range columns {
					//u.Debugf("col:   %#v", col)
					if col.Guard != nil {
						ifColValue, ok := vm.Eval(mt, col.Guard)
						if !ok {
							u.Errorf("Could not evaluate if:   %v", col.Guard.String())
							//return fmt.Errorf("Could not evaluate if clause: %v", col.Guard.String())
						}
						//u.Debugf("if eval val:  %T:%v", ifColValue, ifColValue)
						switch ifColVal := ifColValue.(type) {
						case value.BoolValue:
							if ifColVal.Val() == false {
								//u.Debugf("Filtering out col")
								continue
							}
						}
					}
					if col.Star {
						for k, v := range mt.Row() {
							writeContext.Put(&rel.Column{As: k}, nil, v)
						}
					} else {
						//u.Debugf("tree.Root: as?%v %#v", col.As, col.Expr)
						v, ok := vm.Eval(mt, col.Expr)
						//u.Debugf("evaled: ok?%v key=%v  val=%v", ok, col.Key(), v)
						if ok {
							writeContext.Put(col, mt, v)
						}
					}

				}
		*/
		default:
			u.Errorf("could not project msg:  %T", msg)
		}

		//u.Debugf("completed projection for: %p %#v", out, outMsg)
		select {
		case out <- outMsg:
			return true
		case <-m.SigChan():
			return false
		}
	}
}
