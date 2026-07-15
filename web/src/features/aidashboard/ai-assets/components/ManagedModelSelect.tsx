import { ReloadOutlined } from "@ant-design/icons";
import { Button, Select, Spin } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { fetchAvailableModels } from "../../api/client";
import { useAuth } from "@/shared/auth/authContext";

interface ManagedModelSelectProps {
  allowClear?: boolean;
  disabled?: boolean;
  onChange?: (value?: string) => void;
  placeholder?: string;
  preservedModelId?: string;
  value?: string;
}

export function ManagedModelSelect({
  allowClear,
  disabled,
  onChange,
  placeholder = "选择模型",
  preservedModelId,
  value
}: ManagedModelSelectProps) {
  const { user } = useAuth();
  const modelsQuery = useQuery({
    queryKey: ["available-models", user?.id],
    queryFn: fetchAvailableModels,
    enabled: Boolean(user?.id),
    retry: false,
    staleTime: 60_000
  });

  const models = useMemo(
    () => modelsQuery.data?.models.map((model) => model.trim()).filter(Boolean) ?? [],
    [modelsQuery.data]
  );
  const preserved = (value || preservedModelId || "").trim();
  const preservedUnavailable =
    Boolean(preserved) && modelsQuery.isSuccess && !models.includes(preserved);
  const options = useMemo(() => {
    const items = models.map((model) => ({ label: model, value: model }));
    if (preserved && !models.includes(preserved)) {
      items.unshift({ label: `${preserved}（当前配置）`, value: preserved });
    }
    return items;
  }, [models, preserved]);

  return (
    <div className="ai-assets-model-select">
      <Select
        allowClear={allowClear}
        aria-label="模型"
        disabled={disabled}
        loading={modelsQuery.isLoading}
        notFoundContent={modelsQuery.isLoading ? <Spin size="small" /> : "当前账号没有可用模型"}
        optionFilterProp="label"
        options={options}
        placeholder={placeholder}
        showSearch
        value={value || undefined}
        onChange={(nextValue) => onChange?.(nextValue || undefined)}
      />
      {modelsQuery.isError ? (
        <div className="ai-assets-model-select__feedback is-error">
          <span>模型列表加载失败，已有配置仍可保留使用。</span>
          <Button
            icon={<ReloadOutlined />}
            size="small"
            type="text"
            onClick={() => void modelsQuery.refetch()}
          >
            重试
          </Button>
        </div>
      ) : preservedUnavailable ? (
        <div className="ai-assets-model-select__feedback is-warning">
          当前配置的模型不在该账号的可用模型列表中，建议切换后再运行。
        </div>
      ) : modelsQuery.isSuccess && models.length === 0 ? (
        <div className="ai-assets-model-select__feedback is-warning">当前账号没有可用模型。</div>
      ) : null}
    </div>
  );
}
