import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button, Card, Col, Form, InputNumber, Row, Select, Typography } from "antd";
import type { WheelParams, WheelState } from "../api/types";

export interface WheelFormValues {
  full_position_price: number | null;
  zero_position_price: number | null;
  max_inventory: number | null;
  move_interval_pct: number | null;
  min_premium_per_share: number | null;
  stock_switch_pct: number | null;
  trade_gap: number | null;
  min_dte: number | null;
  max_dte: number | null;
  min_option_quality: number | null;
  strategic_state: WheelState | string;
}

export const WHEEL_STATES: WheelState[] = ["NORMAL", "CAUTION", "PAUSE_BUY", "EXIT"];

export const DEFAULT_WHEEL_VALUES: WheelFormValues = {
  full_position_price: null,
  zero_position_price: null,
  max_inventory: null,
  move_interval_pct: 0,
  min_premium_per_share: 0,
  stock_switch_pct: 0,
  trade_gap: 50,
  min_dte: 5,
  max_dte: 10,
  min_option_quality: 0.6,
  strategic_state: "NORMAL",
};

function finiteNumber(value: number | null, label: string): number {
  if (value === null || !Number.isFinite(value)) throw new Error(`${label} 必须是有效数字`);
  return value;
}

function decimalPercent(value: number): number {
  return Number((value / 100).toPrecision(12));
}

export function validateWheelParams(values: WheelFormValues): WheelParams {
  const fullPrice = finiteNumber(values.full_position_price, "满仓价格");
  const zeroPrice = finiteNumber(values.zero_position_price, "清仓价格");
  const maxInventory = finiteNumber(values.max_inventory, "最大库存");
  const moveIntervalPct = finiteNumber(values.move_interval_pct, "再次出手价差");
  const minPremium = finiteNumber(values.min_premium_per_share, "最低每股权利金");
  const stockSwitchPct = finiteNumber(values.stock_switch_pct, "正股切换阈值");
  const tradeGap = finiteNumber(values.trade_gap, "免交易库存差");
  const minDte = finiteNumber(values.min_dte, "最小 DTE");
  const maxDte = finiteNumber(values.max_dte, "最大 DTE");
  const minQuality = finiteNumber(values.min_option_quality, "最低期权质量");
  if (fullPrice <= 0) throw new Error("满仓价格必须大于 0");
  if (zeroPrice <= fullPrice) throw new Error("清仓价格必须大于满仓价格");
  if (maxInventory <= 0 || !Number.isInteger(maxInventory)) throw new Error("最大库存必须是正整数");
  if (moveIntervalPct < 0) throw new Error("再次出手价差必须不小于 0");
  if (minPremium < 0) throw new Error("最低每股权利金必须不小于 0");
  if (stockSwitchPct < 0) throw new Error("正股切换阈值必须不小于 0");
  if (tradeGap < 0) throw new Error("免交易库存差必须不小于 0");
  if (minDte < 5 || maxDte > 10 || !Number.isInteger(minDte) || maxDte < minDte || !Number.isInteger(maxDte)) {
    throw new Error("DTE 必须是 5 到 10 之间的有效范围");
  }
  if (minQuality < 0 || minQuality > 1) throw new Error("最低期权质量必须在 0 到 1 之间");
  if (!WHEEL_STATES.includes(values.strategic_state as WheelState)) throw new Error("战略状态无效");
  return {
    full_position_price: fullPrice,
    zero_position_price: zeroPrice,
    max_inventory: maxInventory,
    move_interval_pct: decimalPercent(moveIntervalPct),
    min_premium_per_share: minPremium,
    stock_switch_pct: decimalPercent(stockSwitchPct),
    trade_gap: tradeGap,
    min_dte: minDte,
    max_dte: maxDte,
    min_option_quality: minQuality,
    strategic_state: values.strategic_state as WheelState,
  };
}

function formValues(values?: Partial<WheelParams>): WheelFormValues {
  return {
    full_position_price: values?.full_position_price ?? DEFAULT_WHEEL_VALUES.full_position_price,
    zero_position_price: values?.zero_position_price ?? DEFAULT_WHEEL_VALUES.zero_position_price,
    max_inventory: values?.max_inventory ?? DEFAULT_WHEEL_VALUES.max_inventory,
    move_interval_pct: values?.move_interval_pct === undefined ? DEFAULT_WHEEL_VALUES.move_interval_pct : values.move_interval_pct * 100,
    min_premium_per_share: values?.min_premium_per_share ?? DEFAULT_WHEEL_VALUES.min_premium_per_share,
    stock_switch_pct: values?.stock_switch_pct === undefined ? DEFAULT_WHEEL_VALUES.stock_switch_pct : values.stock_switch_pct * 100,
    trade_gap: values?.trade_gap ?? DEFAULT_WHEEL_VALUES.trade_gap,
    min_dte: values?.min_dte ?? DEFAULT_WHEEL_VALUES.min_dte,
    max_dte: values?.max_dte ?? DEFAULT_WHEEL_VALUES.max_dte,
    min_option_quality: values?.min_option_quality ?? DEFAULT_WHEEL_VALUES.min_option_quality,
    strategic_state: values?.strategic_state ?? DEFAULT_WHEEL_VALUES.strategic_state,
  };
}

export interface WheelFormProps {
  initialValues?: Partial<WheelParams>;
  onSubmit: (params: WheelParams) => void | Promise<void>;
  submitLabel?: string;
  formId?: string;
  compact?: boolean;
}

export function WheelForm({ initialValues, onSubmit, submitLabel = "保存", formId = "wheel-form", compact = false }: WheelFormProps): ReactNode {
  const [form] = Form.useForm<WheelFormValues>();
  const [error, setError] = useState<string | null>(null);
  const initial = useMemo(() => formValues(initialValues), [initialValues]);
  useEffect(() => {
    form.setFieldsValue(initial);
  }, [form, initial]);

  return (
    <Card className="wheel-form-card" bordered={!compact}>
      {error ? <Alert closable onClose={() => setError(null)} showIcon type="error" message={error} style={{ marginBottom: 16 }} /> : null}
      <Form<WheelFormValues> form={form} id={formId} layout="vertical" initialValues={initial} onFinish={(values) => {
        try {
          const params = validateWheelParams(values);
          setError(null);
          void Promise.resolve(onSubmit(params)).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "保存失败，请重试"));
        } catch (caught: unknown) {
          setError(caught instanceof Error ? caught.message : "请检查表单");
        }
      }}>
        <Typography.Title level={4}>Wheel 配置</Typography.Title>
        <Typography.Paragraph type="secondary">百分比字段按界面中的 % 输入，提交时统一换算为小数。</Typography.Paragraph>
        <Row gutter={[16, 0]}>
          <Col xs={24} sm={12} lg={8}><Form.Item label="满仓价格" name="full_position_price"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="清仓价格" name="zero_position_price"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最大库存" name="max_inventory"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="再次出手价差 (%)" name="move_interval_pct"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最低每股权利金" name="min_premium_per_share"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="正股切换阈值 (%)" name="stock_switch_pct"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="免交易库存差" name="trade_gap"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最小 DTE" name="min_dte"><InputNumber min={5} max={10} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最大 DTE" name="max_dte"><InputNumber min={5} max={10} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最低期权质量" name="min_option_quality"><InputNumber min={0} max={1} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="战略状态" name="strategic_state"><Select options={WHEEL_STATES.map((value) => ({ value, label: value }))} /></Form.Item></Col>
        </Row>
        <Button type="primary" htmlType="submit">{submitLabel}</Button>
      </Form>
    </Card>
  );
}
