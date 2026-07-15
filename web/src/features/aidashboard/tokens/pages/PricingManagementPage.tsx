import {
  CalculatorOutlined,
  CheckOutlined,
  DollarOutlined,
  EditOutlined,
  PlusOutlined,
  SyncOutlined
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  DatePicker,
  Form,
  Input,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  Tag
} from "antd";
import type { FormInstance } from "antd";
import dayjs from "dayjs";
import { useState } from "react";
import type { ReactNode } from "react";

import {
  applyPricingRecalculation,
  fetchExchangeRateVersions,
  fetchModelAliases,
  fetchModelPriceVersions,
  fetchPriceBooks,
  fetchPricingRecalculationRuns,
  fetchTokenAnalyticsCapability,
  fetchUnpricedModels,
  previewPricingRecalculation,
  saveExchangeRateVersion,
  saveModelAlias,
  saveModelPriceVersion,
  savePriceBook
} from "../../api/client";
import type {
  ExchangeRateVersion,
  ModelPriceVersion,
  PriceBook,
  PricingRecalculationRun
} from "../../api/types";
import { useAuth } from "@/shared/auth/authContext";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";

import "./PricingManagementPage.css";

type PriceFormValues = {
  price_book_id: string;
  canonical_model: string;
  billing_variant: string;
  input_per_million: string;
  cache_read_per_million: string;
  cache_write_5m_per_million: string;
  cache_write_1h_per_million: string;
  output_per_million: string;
  effective_from: dayjs.Dayjs;
  effective_to?: dayjs.Dayjs;
  source_url?: string;
  notes?: string;
  status: "draft" | "published";
};

type RateFormValues = {
  rate: string;
  effective_from: dayjs.Dayjs;
  effective_to?: dayjs.Dayjs;
  source_url?: string;
  notes?: string;
  status: "draft" | "published";
};

function pricingStatus(status: string) {
  const values: Record<string, { color: string; text: string }> = {
    active: { color: "success", text: "当前使用" },
    draft: { color: "default", text: "草稿" },
    archived: { color: "default", text: "已归档" },
    published: { color: "blue", text: "已发布" },
    pending: { color: "warning", text: "待审核" },
    reviewed: { color: "success", text: "已审核" },
    rejected: { color: "error", text: "已拒绝" }
  };
  const item = values[status] ?? { color: "default", text: status };
  return <Tag color={item.color}>{item.text}</Tag>;
}

export function PricingManagementPage() {
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const [bookOpen, setBookOpen] = useState(false);
  const [aliasOpen, setAliasOpen] = useState(false);
  const [priceOpen, setPriceOpen] = useState(false);
  const [rateOpen, setRateOpen] = useState(false);
  const [recalculateOpen, setRecalculateOpen] = useState(false);
  const [editingBook, setEditingBook] = useState<PriceBook>();
  const [correctingPrice, setCorrectingPrice] = useState<ModelPriceVersion>();
  const [correctingRate, setCorrectingRate] = useState<ExchangeRateVersion>();
  const [preview, setPreview] = useState<Awaited<ReturnType<typeof previewPricingRecalculation>>>();
  const [bookForm] = Form.useForm();
  const [aliasForm] = Form.useForm();
  const [priceForm] = Form.useForm<PriceFormValues>();
  const [rateForm] = Form.useForm<RateFormValues>();
  const [recalculateForm] = Form.useForm();

  const capability = useQuery({
    queryKey: ["token-analytics-capability", user?.id],
    queryFn: fetchTokenAnalyticsCapability,
    staleTime: 60_000
  });
  const enabled = Boolean(capability.data?.can_manage_pricing);
  const books = useQuery({ queryKey: ["price-books"], queryFn: fetchPriceBooks, enabled });
  const aliases = useQuery({ queryKey: ["model-aliases"], queryFn: fetchModelAliases, enabled });
  const prices = useQuery({
    queryKey: ["model-price-versions"],
    queryFn: fetchModelPriceVersions,
    enabled
  });
  const rates = useQuery({
    queryKey: ["exchange-rate-versions"],
    queryFn: fetchExchangeRateVersions,
    enabled
  });
  const unpriced = useQuery({
    queryKey: ["unpriced-models"],
    queryFn: fetchUnpricedModels,
    enabled
  });
  const recalculationRuns = useQuery({
    queryKey: ["pricing-recalculation-runs"],
    queryFn: fetchPricingRecalculationRuns,
    enabled
  });

  const refreshPricing = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["price-books"] }),
      queryClient.invalidateQueries({ queryKey: ["model-aliases"] }),
      queryClient.invalidateQueries({ queryKey: ["model-price-versions"] }),
      queryClient.invalidateQueries({ queryKey: ["exchange-rate-versions"] }),
      queryClient.invalidateQueries({ queryKey: ["unpriced-models"] }),
      queryClient.invalidateQueries({ queryKey: ["pricing-recalculation-runs"] })
    ]);
  };
  const bookMutation = useMutation({
    mutationFn: savePriceBook,
    onSuccess: async () => {
      setBookOpen(false);
      await refreshPricing();
      void message.success("价格手册已保存");
    }
  });
  const aliasMutation = useMutation({
    mutationFn: saveModelAlias,
    onSuccess: async () => {
      setAliasOpen(false);
      await refreshPricing();
      void message.success("模型映射已保存");
    }
  });
  const priceMutation = useMutation({
    mutationFn: saveModelPriceVersion,
    onSuccess: async () => {
      setPriceOpen(false);
      setCorrectingPrice(undefined);
      await refreshPricing();
      void message.success("模型价格版本已保存");
    }
  });
  const rateMutation = useMutation({
    mutationFn: saveExchangeRateVersion,
    onSuccess: async () => {
      setRateOpen(false);
      setCorrectingRate(undefined);
      await refreshPricing();
      void message.success("汇率版本已保存");
    }
  });
  const recalculateMutation = useMutation({
    mutationFn: applyPricingRecalculation,
    onSuccess: async (result) => {
      setRecalculateOpen(false);
      setPreview(undefined);
      await refreshPricing();
      void message.success(`重算完成，更新 ${result.changed_components} 条成本`);
    }
  });

  if (!capability.isLoading && !enabled) {
    return (
      <PagePanel title="价格管理">
        <Alert type="info" showIcon message="该功能尚未对当前账号开放" />
      </PagePanel>
    );
  }

  const activeBook = books.data?.find((item) => item.status === "active");
  return (
    <PagePanel
      title="价格管理"
      description="维护平台审核后的模型价格、汇率与重算记录"
      breadcrumbs={[{ title: "价格管理" }]}
      showNav={false}
      className="pricing-management-page"
    >
      <Alert
        type="info"
        showIcon
        message="这里计算官方 API 等价成本，不代表会员订阅或企业合同账单。"
      />
      <Tabs
        items={[
          {
            key: "books",
            label: "价格手册",
            children: (
              <PricingSection
                title="价格手册"
                action={
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => {
                      setEditingBook(undefined);
                      bookForm.resetFields();
                      bookForm.setFieldsValue({ status: "draft" });
                      setBookOpen(true);
                    }}
                  >
                    新建
                  </Button>
                }
              >
                <Table<PriceBook>
                  rowKey="id"
                  loading={books.isLoading}
                  dataSource={books.data ?? []}
                  pagination={false}
                  columns={[
                    { title: "名称", dataIndex: "name" },
                    {
                      title: "计价口径",
                      dataIndex: "pricing_basis",
                      render: () => "官方 API 等价成本"
                    },
                    {
                      title: "币种",
                      render: (_, row) => `${row.source_currency} → ${row.display_currency}`
                    },
                    { title: "状态", dataIndex: "status", render: pricingStatus },
                    {
                      title: "操作",
                      width: 120,
                      render: (_, row) => (
                        <Button
                          type="text"
                          icon={<EditOutlined />}
                          onClick={() => {
                            setEditingBook(row);
                            bookForm.setFieldsValue(row);
                            setBookOpen(true);
                          }}
                        >
                          编辑
                        </Button>
                      )
                    }
                  ]}
                />
              </PricingSection>
            )
          },
          {
            key: "aliases",
            label: "模型映射",
            children: (
              <PricingSection
                title="模型映射"
                action={
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => {
                      aliasForm.resetFields();
                      aliasForm.setFieldsValue({ status: "pending" });
                      setAliasOpen(true);
                    }}
                  >
                    新增映射
                  </Button>
                }
              >
                <Table
                  rowKey="id"
                  loading={aliases.isLoading}
                  dataSource={aliases.data ?? []}
                  pagination={false}
                  columns={[
                    { title: "Provider", dataIndex: "provider" },
                    { title: "原始模型", dataIndex: "raw_model_pattern" },
                    { title: "标准模型", dataIndex: "canonical_model" },
                    { title: "状态", dataIndex: "status", render: pricingStatus },
                    {
                      title: "操作",
                      width: 110,
                      render: (_, row) =>
                        row.status === "pending" ? (
                          <Button
                            type="text"
                            icon={<CheckOutlined />}
                            onClick={() => aliasMutation.mutate({ ...row, status: "reviewed" })}
                          >
                            审核
                          </Button>
                        ) : null
                    }
                  ]}
                />
              </PricingSection>
            )
          },
          {
            key: "prices",
            label: "模型价格",
            children: (
              <PricingSection
                title="模型价格版本"
                action={
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    disabled={!activeBook}
                    onClick={() => {
                      setCorrectingPrice(undefined);
                      priceForm.resetFields();
                      priceForm.setFieldsValue({
                        price_book_id: activeBook?.id,
                        billing_variant: "any",
                        status: "draft",
                        effective_from: dayjs()
                      });
                      setPriceOpen(true);
                    }}
                  >
                    新增价格
                  </Button>
                }
              >
                {!activeBook ? (
                  <Alert type="warning" showIcon message="请先启用一个价格手册" />
                ) : null}
                <Table<ModelPriceVersion>
                  rowKey="id"
                  loading={prices.isLoading}
                  dataSource={prices.data ?? []}
                  scroll={{ x: 1300 }}
                  columns={[
                    { title: "模型", dataIndex: "canonical_model", fixed: "left", width: 180 },
                    { title: "Variant", dataIndex: "billing_variant", width: 90 },
                    { title: "Input / 1M", dataIndex: "input_per_million", width: 120 },
                    { title: "Cache Read / 1M", dataIndex: "cache_read_per_million", width: 140 },
                    { title: "Cache 5m / 1M", dataIndex: "cache_write_5m_per_million", width: 140 },
                    { title: "Cache 1h / 1M", dataIndex: "cache_write_1h_per_million", width: 140 },
                    { title: "Output / 1M", dataIndex: "output_per_million", width: 120 },
                    {
                      title: "生效",
                      width: 190,
                      render: (_, row) => `${row.effective_from} ~ ${row.effective_to ?? "长期"}`
                    },
                    { title: "状态", dataIndex: "status", render: pricingStatus, width: 90 },
                    {
                      title: "操作",
                      fixed: "right",
                      width: 100,
                      render: (_, row) =>
                        row.status === "published" && !row.superseded_at ? (
                          <Button
                            type="text"
                            onClick={() =>
                              openPriceCorrection(row, priceForm, setCorrectingPrice, setPriceOpen)
                            }
                          >
                            修正
                          </Button>
                        ) : null
                    }
                  ]}
                />
              </PricingSection>
            )
          },
          {
            key: "rates",
            label: "USD/CNY 汇率",
            children: (
              <PricingSection
                title="USD/CNY 汇率版本"
                action={
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => {
                      setCorrectingRate(undefined);
                      rateForm.resetFields();
                      rateForm.setFieldsValue({ status: "draft", effective_from: dayjs() });
                      setRateOpen(true);
                    }}
                  >
                    新增汇率
                  </Button>
                }
              >
                <Table<ExchangeRateVersion>
                  rowKey="id"
                  loading={rates.isLoading}
                  dataSource={rates.data ?? []}
                  pagination={false}
                  columns={[
                    { title: "汇率", dataIndex: "rate" },
                    {
                      title: "生效",
                      render: (_, row) => `${row.effective_from} ~ ${row.effective_to ?? "长期"}`
                    },
                    {
                      title: "来源",
                      dataIndex: "source_url",
                      render: (value?: string) => value || "-"
                    },
                    { title: "状态", dataIndex: "status", render: pricingStatus },
                    {
                      title: "操作",
                      width: 100,
                      render: (_, row) =>
                        row.status === "published" && !row.superseded_at ? (
                          <Button
                            type="text"
                            onClick={() =>
                              openRateCorrection(row, rateForm, setCorrectingRate, setRateOpen)
                            }
                          >
                            修正
                          </Button>
                        ) : null
                    }
                  ]}
                />
              </PricingSection>
            )
          },
          {
            key: "unpriced",
            label: `未计价模型${unpriced.data?.length ? ` (${unpriced.data.length})` : ""}`,
            children: (
              <PricingSection
                title="未计价模型"
                action={
                  <Button
                    icon={<CalculatorOutlined />}
                    onClick={() => {
                      recalculateForm.resetFields();
                      setPreview(undefined);
                      setRecalculateOpen(true);
                    }}
                  >
                    成本重算
                  </Button>
                }
              >
                <Table
                  rowKey={(row) => `${row.provider}:${row.model}`}
                  loading={unpriced.isLoading}
                  dataSource={unpriced.data ?? []}
                  pagination={false}
                  columns={[
                    { title: "Provider", dataIndex: "provider" },
                    { title: "模型", dataIndex: "model" },
                    { title: "Token", dataIndex: "total_tokens" },
                    { title: "组件数", dataIndex: "component_count" },
                    { title: "最近活动", dataIndex: "last_activity_date" }
                  ]}
                />
              </PricingSection>
            )
          },
          {
            key: "audit",
            label: "重算审计",
            children: (
              <PricingSection title="重算审计">
                <Table<PricingRecalculationRun>
                  rowKey="id"
                  loading={recalculationRuns.isLoading}
                  dataSource={recalculationRuns.data ?? []}
                  pagination={{ pageSize: 20, showSizeChanger: false }}
                  scroll={{ x: 960 }}
                  columns={[
                    {
                      title: "执行时间",
                      dataIndex: "created_at",
                      width: 180,
                      render: (value: string) => dayjs(value).format("YYYY-MM-DD HH:mm:ss")
                    },
                    { title: "操作人", dataIndex: "requested_by_name", width: 140 },
                    {
                      title: "范围",
                      width: 230,
                      render: (_: unknown, record) => {
                        const from = record.filter.from
                          ? dayjs(record.filter.from).format("YYYY-MM-DD")
                          : "不限";
                        const to = record.filter.to
                          ? dayjs(record.filter.to).format("YYYY-MM-DD")
                          : "不限";
                        return `${from} ~ ${to}${record.filter.model ? ` · ${record.filter.model}` : ""}`;
                      }
                    },
                    { title: "原因", dataIndex: "reason" },
                    {
                      title: "影响",
                      width: 180,
                      render: (_: unknown, record) =>
                        `更新 ${record.result.changed_components} / ${record.result.eligible_components}`
                    },
                    {
                      title: "计算器",
                      dataIndex: "calculator_version",
                      width: 130
                    }
                  ]}
                />
              </PricingSection>
            )
          }
        ]}
      />

      <Modal
        className="pricing-management-modal"
        title={editingBook ? "编辑价格手册" : "新建价格手册"}
        open={bookOpen}
        onCancel={() => setBookOpen(false)}
        onOk={() => bookForm.submit()}
        confirmLoading={bookMutation.isPending}
      >
        <Form
          form={bookForm}
          layout="vertical"
          onFinish={(values) =>
            bookMutation.mutate({ id: editingBook?.id, name: values.name, status: values.status })
          }
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select
              options={[
                { label: "草稿", value: "draft" },
                { label: "当前使用", value: "active" },
                { label: "归档", value: "archived" }
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        className="pricing-management-modal"
        title="新增模型映射"
        open={aliasOpen}
        onCancel={() => setAliasOpen(false)}
        onOk={() => aliasForm.submit()}
        confirmLoading={aliasMutation.isPending}
      >
        <Form
          form={aliasForm}
          layout="vertical"
          onFinish={(values) => aliasMutation.mutate(values)}
        >
          <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
            <Input placeholder="codex / claude_code" />
          </Form.Item>
          <Form.Item name="raw_model_pattern" label="原始模型" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="canonical_model" label="标准模型" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="status" label="审核状态" rules={[{ required: true }]}>
            <Select
              options={[
                { label: "待审核", value: "pending" },
                { label: "审核通过", value: "reviewed" },
                { label: "拒绝", value: "rejected" }
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        className="pricing-management-modal"
        title={correctingPrice ? "修正模型价格" : "新增模型价格"}
        open={priceOpen}
        onCancel={() => setPriceOpen(false)}
        onOk={() => priceForm.submit()}
        width={760}
        confirmLoading={priceMutation.isPending}
      >
        <Form<PriceFormValues>
          form={priceForm}
          layout="vertical"
          onFinish={(values) =>
            priceMutation.mutate({
              ...values,
              effective_from: values.effective_from.format("YYYY-MM-DD"),
              effective_to: values.effective_to?.format("YYYY-MM-DD"),
              supersedes_id: correctingPrice?.id
            })
          }
        >
          <div className="pricing-form-grid">
            <Form.Item name="price_book_id" label="价格手册" rules={[{ required: true }]}>
              <Select
                options={(books.data ?? []).map((item) => ({ label: item.name, value: item.id }))}
              />
            </Form.Item>
            <Form.Item name="canonical_model" label="标准模型" rules={[{ required: true }]}>
              <Input disabled={Boolean(correctingPrice)} />
            </Form.Item>
            <Form.Item name="billing_variant" label="Billing Variant" rules={[{ required: true }]}>
              <Select
                disabled={Boolean(correctingPrice)}
                options={[
                  { label: "通用", value: "any" },
                  { label: "5 分钟缓存", value: "5m" },
                  { label: "1 小时缓存", value: "1h" },
                  { label: "未知", value: "unknown" }
                ]}
              />
            </Form.Item>
            <Form.Item name="status" label="状态" rules={[{ required: true }]}>
              <Select
                disabled={Boolean(correctingPrice)}
                options={[
                  { label: "草稿", value: "draft" },
                  { label: "发布", value: "published" }
                ]}
              />
            </Form.Item>
            <Form.Item
              name="input_per_million"
              label="Input / 1M (USD)"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="output_per_million"
              label="Output / 1M (USD)"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="cache_read_per_million"
              label="Cache Read / 1M"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="cache_write_5m_per_million"
              label="Cache Write 5m / 1M"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="cache_write_1h_per_million"
              label="Cache Write 1h / 1M"
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item name="effective_from" label="生效日期" rules={[{ required: true }]}>
              <DatePicker disabled={Boolean(correctingPrice)} />
            </Form.Item>
            <Form.Item name="effective_to" label="失效日期">
              <DatePicker disabled={Boolean(correctingPrice)} />
            </Form.Item>
            <Form.Item name="source_url" label="来源链接">
              <Input />
            </Form.Item>
          </div>
          <Form.Item name="notes" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        className="pricing-management-modal"
        title={correctingRate ? "修正汇率" : "新增汇率"}
        open={rateOpen}
        onCancel={() => setRateOpen(false)}
        onOk={() => rateForm.submit()}
        confirmLoading={rateMutation.isPending}
      >
        <Form<RateFormValues>
          form={rateForm}
          layout="vertical"
          onFinish={(values) =>
            rateMutation.mutate({
              ...values,
              effective_from: values.effective_from.format("YYYY-MM-DD"),
              effective_to: values.effective_to?.format("YYYY-MM-DD"),
              supersedes_id: correctingRate?.id
            })
          }
        >
          <Form.Item name="rate" label="USD/CNY" rules={[{ required: true }]}>
            <Input prefix={<DollarOutlined />} />
          </Form.Item>
          <Space align="start">
            <Form.Item name="effective_from" label="生效日期" rules={[{ required: true }]}>
              <DatePicker disabled={Boolean(correctingRate)} />
            </Form.Item>
            <Form.Item name="effective_to" label="失效日期">
              <DatePicker disabled={Boolean(correctingRate)} />
            </Form.Item>
          </Space>
          <Form.Item name="source_url" label="来源链接">
            <Input />
          </Form.Item>
          <Form.Item name="notes" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select
              disabled={Boolean(correctingRate)}
              options={[
                { label: "草稿", value: "draft" },
                { label: "发布", value: "published" }
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        className="pricing-management-modal"
        title="成本重算"
        open={recalculateOpen}
        onCancel={() => setRecalculateOpen(false)}
        footer={[
          <Button key="cancel" onClick={() => setRecalculateOpen(false)}>
            取消
          </Button>,
          <Button
            key="preview"
            icon={<SyncOutlined />}
            onClick={async () => {
              const values = recalculateForm.getFieldsValue();
              const result = await previewPricingRecalculation({
                from: values.range?.[0]?.format("YYYY-MM-DD"),
                to: values.range?.[1]?.format("YYYY-MM-DD"),
                model: values.model
              });
              setPreview(result);
            }}
          >
            预览影响
          </Button>,
          <Button
            key="apply"
            type="primary"
            disabled={!preview}
            loading={recalculateMutation.isPending}
            onClick={async () => {
              const values = await recalculateForm.validateFields();
              recalculateMutation.mutate({
                from: values.range?.[0]?.format("YYYY-MM-DD"),
                to: values.range?.[1]?.format("YYYY-MM-DD"),
                model: values.model,
                reason: values.reason
              });
            }}
          >
            确认重算
          </Button>
        ]}
      >
        <Form form={recalculateForm} layout="vertical" onValuesChange={() => setPreview(undefined)}>
          <Form.Item name="range" label="活动日期范围">
            <DatePicker.RangePicker />
          </Form.Item>
          <Form.Item name="model" label="标准模型">
            <Input allowClear />
          </Form.Item>
          <Form.Item name="reason" label="重算原因" rules={[{ required: true }]}>
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
        {preview ? (
          <Alert
            type="warning"
            showIcon
            message={`将更新 ${preview.changed_components} 条，保持 ${preview.unchanged_components} 条不变`}
            description={`可计价 ${preview.priced_components} 条，未计价 ${preview.unpriced_components} 条。`}
          />
        ) : null}
      </Modal>
    </PagePanel>
  );
}

function PricingSection({
  title,
  action,
  children
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="pricing-section">
      <header>
        <h2>{title}</h2>
        {action}
      </header>
      <div className="pricing-section__body">{children}</div>
    </section>
  );
}

function openPriceCorrection(
  row: ModelPriceVersion,
  form: FormInstance<PriceFormValues>,
  setCorrecting: (value: ModelPriceVersion) => void,
  setOpen: (value: boolean) => void
) {
  setCorrecting(row);
  form.setFieldsValue({
    ...row,
    status: "published",
    effective_from: dayjs(row.effective_from),
    effective_to: row.effective_to ? dayjs(row.effective_to) : undefined
  });
  setOpen(true);
}

function openRateCorrection(
  row: ExchangeRateVersion,
  form: FormInstance<RateFormValues>,
  setCorrecting: (value: ExchangeRateVersion) => void,
  setOpen: (value: boolean) => void
) {
  setCorrecting(row);
  form.setFieldsValue({
    ...row,
    status: "published",
    effective_from: dayjs(row.effective_from),
    effective_to: row.effective_to ? dayjs(row.effective_to) : undefined
  });
  setOpen(true);
}
