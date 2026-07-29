import { DeleteOutlined, EditOutlined, FolderOpenOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Empty, Input, Modal, Popconfirm, Space, Spin, Tooltip } from "antd";
import { useState } from "react";

import {
  createTeamSyncPath,
  deleteTeamSyncPath,
  fetchTeamSyncPaths,
  updateTeamSyncPath
} from "../../api/client";
import { TokenAnalyticsPage } from "./TokenAnalyticsPage";

import "./TokensPage.css";

export function TokensPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [pathsOpen, setPathsOpen] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [editingID, setEditingID] = useState<string | null>(null);
  const [editingPath, setEditingPath] = useState("");

  const pathsQuery = useQuery({
    queryKey: ["team-sync-paths"],
    queryFn: fetchTeamSyncPaths,
    enabled: pathsOpen,
    staleTime: 30_000
  });

  const refreshPaths = async () => {
    await queryClient.invalidateQueries({ queryKey: ["team-sync-paths"] });
  };

  const createMutation = useMutation({
    mutationFn: createTeamSyncPath,
    onSuccess: async () => {
      setNewPath("");
      await refreshPaths();
      message.success("同步目录已添加");
    },
    onError: () => message.error("目录保存失败，请检查路径是否与其他成员冲突")
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, path }: { id: string; path: string }) => updateTeamSyncPath(id, path),
    onSuccess: async () => {
      setEditingID(null);
      setEditingPath("");
      await refreshPaths();
      message.success("同步目录已更新");
    },
    onError: () => message.error("目录更新失败，请检查路径是否与其他成员冲突")
  });

  const deleteMutation = useMutation({
    mutationFn: deleteTeamSyncPath,
    onSuccess: async () => {
      await refreshPaths();
      message.success("同步目录已删除，后续团队同步将停止处理该目录");
    },
    onError: () => message.error("目录删除失败，请稍后重试")
  });

  const items = pathsQuery.data?.items ?? [];

  const teamSyncPathButton = (
    <Tooltip title="团队同步目录">
      <Button
        className="team-sync-paths-trigger"
        type="text"
        shape="circle"
        icon={<FolderOpenOutlined />}
        aria-label="配置团队同步目录"
        onClick={() => setPathsOpen(true)}
      />
    </Tooltip>
  );

  return (
    <div className="tokens-page">
      <TokenAnalyticsPage scope="mine" toolbarExtra={teamSyncPathButton} />

      <Modal
        className="team-sync-paths-modal"
        title="团队同步目录"
        open={pathsOpen}
        width={680}
        footer={null}
        destroyOnHidden
        onCancel={() => {
          setPathsOpen(false);
          setEditingID(null);
          setEditingPath("");
        }}
      >
        <div className="team-sync-paths__body">
          <div className="team-sync-paths__intro">
            仅用于共享开发机的团队同步。目录决定新 Session 的个人归属，已上传的 Session
            不会随配置变化自动迁移。
          </div>

          <div className="team-sync-paths__create">
            <Input
              value={newPath}
              placeholder="输入绝对路径，例如 /home/shared/alice/project"
              onChange={(event) => setNewPath(event.target.value)}
              onPressEnter={() => {
                const value = newPath.trim();
                if (value) createMutation.mutate(value);
              }}
            />
            <Button
              type="primary"
              icon={<PlusOutlined />}
              loading={createMutation.isPending}
              disabled={!newPath.trim()}
              onClick={() => createMutation.mutate(newPath.trim())}
            >
              添加
            </Button>
          </div>

          {pathsQuery.isLoading ? (
            <div className="team-sync-paths__loading">
              <Spin size="small" />
            </div>
          ) : pathsQuery.isError ? (
            <Alert type="error" showIcon title="同步目录加载失败" />
          ) : items.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未配置同步目录" />
          ) : (
            <div className="team-sync-paths__list">
              {items.map((item) => (
                <div className="team-sync-paths__item" key={item.id}>
                  {editingID === item.id ? (
                    <Input
                      value={editingPath}
                      autoFocus
                      onChange={(event) => setEditingPath(event.target.value)}
                      onPressEnter={() => {
                        const value = editingPath.trim();
                        if (value) updateMutation.mutate({ id: item.id, path: value });
                      }}
                    />
                  ) : (
                    <code>{item.normalized_path}</code>
                  )}
                  <Space size={4}>
                    {editingID === item.id ? (
                      <>
                        <Button
                          size="small"
                          type="primary"
                          loading={updateMutation.isPending}
                          disabled={!editingPath.trim()}
                          onClick={() =>
                            updateMutation.mutate({ id: item.id, path: editingPath.trim() })
                          }
                        >
                          保存
                        </Button>
                        <Button size="small" onClick={() => setEditingID(null)}>
                          取消
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button
                          size="small"
                          type="text"
                          icon={<EditOutlined />}
                          aria-label="修改同步目录"
                          onClick={() => {
                            setEditingID(item.id);
                            setEditingPath(item.normalized_path);
                          }}
                        />
                        <Popconfirm
                          title="删除此同步目录？"
                          description="删除后，该目录下新旧 Session 都会停止后续团队同步。"
                          okText="删除"
                          okButtonProps={{ danger: true }}
                          cancelText="取消"
                          onConfirm={() => deleteMutation.mutate(item.id)}
                        >
                          <Button
                            size="small"
                            type="text"
                            danger
                            icon={<DeleteOutlined />}
                            aria-label="删除同步目录"
                          />
                        </Popconfirm>
                      </>
                    )}
                  </Space>
                </div>
              ))}
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
