import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button, Card, Col, Form, InputNumber, Row, Select, Space, Typography } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import type { WheelCurvePoint, WheelParams, WheelState } from "../api/types";

export interface WheelFormValues {
  price_position_curve: Array<{ price: number | null; target_inventory: number | null }>;
  max_inventory: number | null;
  lot_size: number | null;
  min_dte: number | null;
  max_dte: number | null;
  min_option_quality: number | null;
  max_daily_orders: number | null;
  extreme_max_daily_orders: number | null;
  no_trade_gap: number | null;
  strategic_state: WheelState | string;
}

export const WHEEL_STATES: WheelState[] = ["NORMAL", "CAUTION", "PAUSE_BUY", "EXIT"];

export const DEFAULT_WHEEL_VALUES: WheelFormValues = {
  price_position_curve: [
    { price: null, target_inventory: null },
    { price: null, target_inventory: null },
  ],
  max_inventory: null,
  lot_size: 100,
  min_dte: 5,
  max_dte: 10,
  min_option_quality: 0.6,
  max_daily_orders: 1,
  extreme_max_daily_orders: 2,
  no_trade_gap: 50,
  strategic_state: "NORMAL",
};

function finiteNumber(value: number | null, label: string): number {
  if (value === null || !Number.isFinite(value)) throw new Error(`${label} 必须是有效数字`);
  return value;
}

export function validateWheelParams(values: WheelFormValues): WheelParams {
  const maxInventory = finiteNumber(values.max_inventory, "最大库存");
  const lotSize = finiteNumber(values.lot_size, "合约乘数");
  const minDte = finiteNumber(values.min_dte, "最小 DTE");
  const maxDte = finiteNumber(values.max_dte, "最大 DTE");
  const minQuality = finiteNumber(values.min_option_quality, "最低期权质量");
  const maxDaily = finiteNumber(values.max_daily_orders, "正常日最多张数");
  const extremeDaily = finiteNumber(values.extreme_max_daily_orders, "极端日最多张数");
  const noTradeGap = finiteNumber(values.no_trade_gap, "不交易缺口");
  if (maxInventory <= 0) throw new Error("最大库存必须大于 0");
  if (lotSize < 1 || !Number.isInteger(lotSize)) throw new Error("合约乘数必须是正整数");
  if (minDte < 5 || maxDte > 10 || !Number.isInteger(minDte) || maxDte < minDte || !Number.isInteger(maxDte)) {
    throw new Error("DTE 必须是 5 到 10 之间的有效范围");
  }
  if (minQuality < 0 || minQuality > 1) throw new Error("最低期权质量必须在 0 到 1 之间");
  if (maxDaily !== 1) throw new Error("正常日最多张数固定为 1");
  if (extremeDaily < 1 || extremeDaily > 2 || !Number.isInteger(extremeDaily) || extremeDaily < maxDaily) {
    throw new Error("极端日最多张数必须在 1 到 2 之间");
  }
  if (noTradeGap < 0) throw new Error("不交易缺口必须不小于 0");
  if (values.price_position_curve.length < 2) throw new Error("至少需要两个价格锚点");
  const curve: WheelCurvePoint[] = [];
  let previousPrice = Number.NEGATIVE_INFINITY;
  let previousInventory = Number.POSITIVE_INFINITY;
  for (const [index, point] of values.price_position_curve.entries()) {
    if (point.price === null || point.target_inventory === null || !Number.isFinite(point.price) || !Number.isFinite(point.target_inventory)) {
      throw new Error(`曲线第 ${index + 1} 行必须填写有效数字`);
    }
    const price = finiteNumber(point.price, `曲线第 ${index + 1} 行`);
    const inventory = finiteNumber(point.target_inventory, `曲线第 ${index + 1} 行`);
    if (price <= 0) throw new Error("曲线价格必须大于 0");
    if (price <= previousPrice) throw new Error("曲线价格必须严格递增");
    if (inventory > previousInventory) throw new Error("曲线目标库存必须单调不增");
    if (inventory < 0 || inventory > maxInventory) throw new Error("曲线目标库存必须位于 0 与最大库存之间");
    previousPrice = price;
    previousInventory = inventory;
    curve.push({ price, target_inventory: inventory });
  }
  if (!WHEEL_STATES.includes(values.strategic_state as WheelState)) throw new Error("战略状态无效");
  return {
    price_position_curve: curve,
    max_inventory: maxInventory,
    lot_size: lotSize,
    min_dte: minDte,
    max_dte: maxDte,
    min_option_quality: minQuality,
    max_daily_orders: maxDaily,
    extreme_max_daily_orders: extremeDaily,
    no_trade_gap: noTradeGap,
    strategic_state: values.strategic_state as WheelState,
  };
}

function formValues(values?: Partial<WheelParams>): WheelFormValues {
  const curve = values?.price_position_curve?.length ? values.price_position_curve : DEFAULT_WHEEL_VALUES.price_position_curve;
  return {
    price_position_curve: curve.map((point) => ({ price: point.price, target_inventory: point.target_inventory })),
    max_inventory: values?.max_inventory ?? DEFAULT_WHEEL_VALUES.max_inventory,
    lot_size: values?.lot_size ?? DEFAULT_WHEEL_VALUES.lot_size,
    min_dte: values?.min_dte ?? DEFAULT_WHEEL_VALUES.min_dte,
    max_dte: values?.max_dte ?? DEFAULT_WHEEL_VALUES.max_dte,
    min_option_quality: values?.min_option_quality ?? DEFAULT_WHEEL_VALUES.min_option_quality,
    max_daily_orders: values?.max_daily_orders ?? DEFAULT_WHEEL_VALUES.max_daily_orders,
    extreme_max_daily_orders: values?.extreme_max_daily_orders ?? DEFAULT_WHEEL_VALUES.extreme_max_daily_orders,
    no_trade_gap: values?.no_trade_gap ?? DEFAULT_WHEEL_VALUES.no_trade_gap,
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
        <Typography.Paragraph type="secondary">价格必须严格递增，目标库存必须单调不增且位于 0 与最大库存之间。</Typography.Paragraph>
        <Form.List name="price_position_curve">
          {(fields, { add, remove }) => (
            <>
              <Space align="center" style={{ marginBottom: 12 }}>
                <Typography.Text strong>价格—目标库存曲线</Typography.Text>
                <Button type="dashed" icon={<PlusOutlined />} onClick={() => {
                  const current = form.getFieldValue("price_position_curve") as WheelFormValues["price_position_curve"];
                  const last = current.at(-1);
                  add({ price: last?.price === null || last?.price === undefined ? null : last.price + 1, target_inventory: last?.target_inventory ?? null });
                }}>添加锚点</Button>
              </Space>
              {fields.map((field, index) => (
                <div className="wheel-curve-row" key={field.key}>
                  <Form.Item label={`价格 ${index + 1}`} name={[field.name, "price"]}>
                    <InputNumber min={0} step="any" placeholder="例如 400" style={{ width: "100%" }} />
                  </Form.Item>
                  <Form.Item label={`目标库存 ${index + 1}`} name={[field.name, "target_inventory"]}>
                    <InputNumber min={0} step="any" placeholder="例如 1200" style={{ width: "100%" }} />
                  </Form.Item>
                  <Button aria-label={`移除第 ${index + 1} 个锚点`} disabled={fields.length <= 1} icon={<DeleteOutlined />} onClick={() => remove(field.name)} />
                </div>
              ))}
            </>
          )}
        </Form.List>
        <Row gutter={[16, 0]}>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最大库存" name="max_inventory"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="合约乘数" name="lot_size"><InputNumber min={1} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最小 DTE" name="min_dte"><InputNumber min={5} max={10} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最大 DTE" name="max_dte"><InputNumber min={5} max={10} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最低期权质量" name="min_option_quality"><InputNumber min={0} max={1} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="正常日最多张数" name="max_daily_orders"><InputNumber min={1} max={1} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="极端日最多张数" name="extreme_max_daily_orders"><InputNumber min={1} max={2} step={1} style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="不交易缺口" name="no_trade_gap"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="战略状态" name="strategic_state"><Select options={WHEEL_STATES.map((value) => ({ value, label: value }))} /></Form.Item></Col>
        </Row>
        <Button type="primary" htmlType="submit">{submitLabel}</Button>
      </Form>
    </Card>
  );
}
