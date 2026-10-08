// Package indicators 提供资源状态指示器数据模型：
// 描述资源当前值与目标值（期望值）的比较语义，供各 tracker 判定就绪/失败。
// 本包只做纯数据建模，不做任何终端渲染。
package indicators

import "fmt"

// StringEqualConditionIndicator 字符串相等条件指示器。
type StringEqualConditionIndicator struct {
	Value       string
	TargetValue string
	FailedValue string

	forceReady  *bool
	forceFailed *bool
}

func (indicator *StringEqualConditionIndicator) IsProgressing(prevIndicator *StringEqualConditionIndicator) bool {
	return (prevIndicator != nil) && (indicator.Value != prevIndicator.Value)
}

func (indicator *StringEqualConditionIndicator) SetReady(ready bool) {
	indicator.forceReady = &ready
}

func (indicator *StringEqualConditionIndicator) IsReady() bool {
	if indicator.forceReady != nil {
		return *indicator.forceReady
	}

	return indicator.Value == indicator.TargetValue
}

func (indicator *StringEqualConditionIndicator) SetFailed(failed bool) {
	indicator.forceFailed = &failed
}

func (indicator *StringEqualConditionIndicator) IsFailed() bool {
	if indicator.forceFailed != nil {
		return *indicator.forceFailed
	}

	return indicator.Value == indicator.FailedValue
}

func (indicator *StringEqualConditionIndicator) formatValue(withTargetValue bool) string {
	if withTargetValue {
		return fmt.Sprintf("%s (%s)", indicator.Value, indicator.TargetValue)
	}
	return fmt.Sprintf("%s", indicator.Value)
}

// FormatValue 返回 "value (target)" 或 "value" 的可读形式。
func (indicator *StringEqualConditionIndicator) FormatValue(withTargetValue bool) string {
	return indicator.formatValue(withTargetValue)
}

// Int32EqualConditionIndicator int32 相等条件指示器。
type Int32EqualConditionIndicator struct {
	Value       int32
	TargetValue int32
}

func (indicator *Int32EqualConditionIndicator) formatValue(withTargetValue bool) string {
	if withTargetValue {
		return fmt.Sprintf("%d/%d", indicator.Value, indicator.TargetValue)
	}
	return fmt.Sprintf("%d", indicator.Value)
}

func (indicator *Int32EqualConditionIndicator) IsProgressing(prevIndicator *Int32EqualConditionIndicator) bool {
	return (prevIndicator != nil) && (indicator.Value != prevIndicator.Value)
}

func (indicator *Int32EqualConditionIndicator) IsReady() bool {
	return indicator.Value == indicator.TargetValue
}

// FormatValue 返回 "value/target" 或 "value" 的可读形式。
func (indicator *Int32EqualConditionIndicator) FormatValue(withTargetValue bool) string {
	return indicator.formatValue(withTargetValue)
}

// Int64GreaterOrEqualConditionIndicator int64 大于等于条件指示器。
type Int64GreaterOrEqualConditionIndicator struct {
	Value       int64
	TargetValue int64
}

func (indicator *Int64GreaterOrEqualConditionIndicator) IsProgressing(prevIndicator *Int64GreaterOrEqualConditionIndicator) bool {
	return (prevIndicator != nil) && (indicator.Value != prevIndicator.Value)
}

func (indicator *Int64GreaterOrEqualConditionIndicator) IsReady() bool {
	return indicator.Value >= indicator.TargetValue
}

func (indicator *Int64GreaterOrEqualConditionIndicator) formatValue(withTargetValue bool) string {
	if withTargetValue {
		return fmt.Sprintf("%d/%d", indicator.Value, indicator.TargetValue)
	}
	return fmt.Sprintf("%d", indicator.Value)
}

// FormatValue 返回 "value/target" 或 "value" 的可读形式。
func (indicator *Int64GreaterOrEqualConditionIndicator) FormatValue(withTargetValue bool) string {
	return indicator.formatValue(withTargetValue)
}

// Int32MultipleEqualConditionIndicator int32 多值相等条件指示器。
type Int32MultipleEqualConditionIndicator struct {
	Value        int32
	TargetValues []int32
}

func (indicator *Int32MultipleEqualConditionIndicator) IsReady() bool {
	for _, val := range indicator.TargetValues {
		if val == indicator.Value {
			return true
		}
	}
	return false
}

func (indicator *Int32MultipleEqualConditionIndicator) IsProgressing(prevIndicator *Int32MultipleEqualConditionIndicator) bool {
	return (prevIndicator != nil) && (indicator.Value != prevIndicator.Value)
}
