import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button, Card, Col, Form, InputNumber, Row, Typography } from "antd";
import type { WheelCurvePoint, WheelParams } from "../api/types";

export interface WheelFormValues {
  price_low: number | null;
  price_high: number | null;
  max_inventory: number | null;
}

export const DEFAULT_WHEEL_VALUES: WheelFormValues = {
  price_low: null,
  price_high: null,
  max_inventory: null,
};

function finiteNumber(value: number | null, label: string): number {
  if (value === null || !Number.isFinite(value)) throw new Error(`${label} 必须是有效数字`);
  return value;
}

// validateWheelParams turns the user-facing 「价格范围 + 最大持仓」 into the
// two-point price_position_curve the engine requires: at price_low the target
// inventory is max_inventory (满仓), at price_high it is 0 (清仓). Every other
// parameter is a default or inferred from the live market (lot size from the
// quote, strike/DTE from the chain), per 老板指令 2026-08-13.
export function validateWheelParams(values: WheelFormValues): WheelParams {
  const maxInventory = finiteNumber(values.max_inventory, "最大库存");
  const priceLow = finiteNumber(values.price_low, "价格下限");
  const priceHigh = finiteNumber(values.price_high, "价格上限");
  if (maxInventory <= 0) throw new Error("最大库存必须大于 0");
  if (priceLow <= 0 || priceHigh <= 0) throw new Error("价格必须大于 0");
  if (priceHigh <= priceLow) throw new Error("价格上限必须大于价格下限");
  const curve: WheelCurvePoint[] = [
    { price: priceLow, target_inventory: maxInventory },
    { price: priceHigh, target_inventory: 0 },
  ];
  return { price_position_curve: curve, max_inventory: maxInventory };
}

// formValues restores the form from saved params. Old multi-point curves
// collapse to their endpoints, preserving the outer range; missing curves
// fall back to the defaults.
function formValues(values?: Partial<WheelParams>): WheelFormValues {
  const curve = values?.price_position_curve?.length ? values.price_position_curve : [];
  const first = curve[0];
  const last = curve[curve.length - 1];
  return {
    price_low: first?.price ?? null,
    price_high: last?.price ?? null,
    max_inventory: values?.max_inventory ?? DEFAULT_WHEEL_VALUES.max_inventory,
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
        <Typography.Paragraph type="secondary">只配置价格范围与最大持仓：价格下限（满仓）、价格上限（清仓），区间内的持仓分配由策略自主决定；合约乘数等行情参数实时拉取。</Typography.Paragraph>
        <Row gutter={[16, 0]}>
          <Col xs={24} sm={12} lg={8}><Form.Item label="价格下限（满仓价）" name="price_low"><InputNumber min={0} step="any" placeholder="例如 25" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="价格上限（清仓价）" name="price_high"><InputNumber min={0} step="any" placeholder="例如 27" style={{ width: "100%" }} /></Form.Item></Col>
          <Col xs={24} sm={12} lg={8}><Form.Item label="最大库存" name="max_inventory"><InputNumber min={0} step="any" style={{ width: "100%" }} /></Form.Item></Col>
        </Row>
        <Button type="primary" htmlType="submit">{submitLabel}</Button>
      </Form>
    </Card>
  );
}
