import {
  CrownOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SolutionOutlined,
  TeamOutlined,
  UserOutlined
} from "@ant-design/icons";
import { Alert, Button, Empty, Form, Input, Modal, Select, Switch, Table, message } from "antd";
import type { TableProps } from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import "../../aidashboard-pattern.css";
import {
  adminBatchAddUsers,
  adminCreateDepartment,
  adminCreateTeam,
  adminDeleteTeam,
  adminUpdateTeam,
  adminUpdateDepartment,
  adminUpdateUser,
  fetchAIHubUsers,
  fetchDepartments,
  fetchTeams,
  fetchUsers
} from "../../api/client";
import type { AIHubUser, Department, Team } from "../../api/types";
import {
  RequirementMetricCard,
  RequirementMetricGrid,
  type RequirementMetricTone
} from "../../requirements/components/RequirementMetricCard";
import { useAuth } from "@/shared/auth/authContext";
import { ROLE_LABELS, type User, type UserRole } from "@/shared/auth/types";
import { PagePanel } from "@/shared/components/PagePanel/PagePanel";
import { getApiErrorMessage } from "@/shared/request/apiError";
import { ResourceTable } from "@/shared/components/ResourceTable/ResourceTable";
import { TableLayout } from "@/shared/components/TableLayout/TableLayout";

import "./OrganizationPage.css";

const ROLE_ORDER: UserRole[] = ["admin", "director", "pm", "team_leader", "employee"];

const ROLE_TONE: Record<UserRole, RequirementMetricTone> = {
  admin: "danger",
  director: "primary",
  pm: "info",
  team_leader: "warning",
  employee: "success"
};

const ROLE_ICON: Record<UserRole, JSX.Element> = {
  admin: <SafetyCertificateOutlined />,
  director: <CrownOutlined />,
  pm: <SolutionOutlined />,
  team_leader: <TeamOutlined />,
  employee: <UserOutlined />
};

const ROLE_OPTIONS = ROLE_ORDER.map((role) => ({ value: role, label: ROLE_LABELS[role] }));
const EMPTY_USERS: User[] = [];
const EMPTY_TEAMS: Team[] = [];

interface AddAIHubUserFormValues {
  app_role: UserRole;
  team_id?: string;
  local_enabled?: boolean;
}

interface CreateTeamFormValues {
  name: string;
  director_user_id?: string;
}
interface DepartmentFormValues { name: string; director_user_id?: string; team_ids: string[]; pm_user_ids: string[]; }

interface RoleTeamChangeFormValues {
  team_id: string;
}

interface PendingRoleTeamChange {
  user: User;
  appRole: UserRole;
}

function displayUser(user: User) {
  return user.nickname?.trim() || user.name?.trim() || user.username || user.employee_id || `用户 ${user.id}`;
}

function displayAIHubUser(user: AIHubUser) {
  return user.nickname?.trim() || user.username || `AIHub 用户 ${user.id}`;
}

function displayAIHubUserWithUsername(user: AIHubUser) {
  const name = displayAIHubUser(user);
  return user.username && user.username !== name ? `${name} (${user.username})` : name;
}

function displayUserWithUsername(user: User) {
  const name = displayUser(user);
  const username = user.username || user.employee_id;
  return username && username !== name ? `${name} (${username})` : name;
}

function initials(name: string) {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  return trimmed.length > 2 ? trimmed.slice(0, 2) : trimmed[0];
}

function avatarToneClass(role: UserRole) {
  if (role === "pm") return "is-pm";
  if (role === "team_leader") return "is-tl";
  return "is-employee";
}

function roleRequiresTeam(role?: UserRole) {
  return role === "employee" || role === "team_leader";
}

function roleForcesNoTeam(role?: UserRole) {
  return role === "admin" || role === "director" || role === "pm";
}

function aidaStatusClass(status?: AIHubUser["aida_status"]) {
  if (status === "active") return "is-added";
  if (status === "disabled") return "is-disabled";
  return "is-not-added";
}

function aidaStatusLabel(user: AIHubUser) {
  return user.aida_status_label || "未添加";
}

export function OrganizationPage() {
  const { user: currentUser } = useAuth();
  const queryClient = useQueryClient();
  const [addUserOpen, setAddUserOpen] = useState(false);
  const [addUserSearch, setAddUserSearch] = useState("");
  const [aihubUsersPage, setAIHubUsersPage] = useState(1);
  const [aihubUsersPageSize, setAIHubUsersPageSize] = useState(10);
  const [selectedAIHubUserIDs, setSelectedAIHubUserIDs] = useState<number[]>([]);
  const [addUserError, setAddUserError] = useState<string>();
  const [addUserForm] = Form.useForm<AddAIHubUserFormValues>();
  const [createTeamOpen, setCreateTeamOpen] = useState(false);
  const [editingTeam, setEditingTeam] = useState<Team | null>(null);
  const [createTeamError, setCreateTeamError] = useState<string>();
  const [createTeamForm] = Form.useForm<CreateTeamFormValues>();
  const [roleTeamChange, setRoleTeamChange] = useState<PendingRoleTeamChange | null>(null);
  const [userSearch, setUserSearch] = useState("");
  const [departmentOpen, setDepartmentOpen] = useState(false);
  const [editingDepartment, setEditingDepartment] = useState<Department | null>(null);
  const [departmentForm] = Form.useForm<DepartmentFormValues>();
  const [roleTeamForm] = Form.useForm<RoleTeamChangeFormValues>();
  const isAdmin = currentUser?.role === "admin";

  const usersQuery = useQuery<User[]>({
    queryKey: ["users"],
    queryFn: () => fetchUsers(),
    enabled: isAdmin,
    staleTime: 60_000
  });
  const teamsQuery = useQuery<Team[]>({
    queryKey: ["teams"],
    queryFn: () => fetchTeams(),
    enabled: isAdmin,
    staleTime: 5 * 60_000
  });
  const departmentsQuery = useQuery<Department[]>({ queryKey: ["departments"], queryFn: fetchDepartments, enabled: isAdmin });
  const aihubUsersQuery = useQuery({
    queryKey: ["aihub-users", addUserSearch, aihubUsersPage, aihubUsersPageSize],
    queryFn: () => fetchAIHubUsers({ search_key: addUserSearch, page_size: aihubUsersPageSize, page_num: aihubUsersPage }),
    enabled: isAdmin && addUserOpen,
    staleTime: 30_000
  });

  const users = usersQuery.data ?? EMPTY_USERS;
  const teams = teamsQuery.data ?? EMPTY_TEAMS;
  const departments = departmentsQuery.data ?? [];
  const aihubUsers = aihubUsersQuery.data?.items ?? [];
  const selectedAIHubUsers = aihubUsers.filter((user) => selectedAIHubUserIDs.includes(user.id));
  const enabledUsers = users.filter((u) => u.local_enabled !== false);
  const visibleUsers = users.filter((u) => {
    const q = userSearch.trim().toLowerCase();
    return !q || [u.nickname, u.name, u.username, u.employee_id, u.email].some(v => v?.toLowerCase().includes(q));
  });
  const teamMembers = (teamID: string) =>
    enabledUsers.filter((u) => u.team_id === teamID && (u.role === "team_leader" || u.role === "employee"));

  const roleCounts = ROLE_ORDER.map((role) => ({
    role,
    count: enabledUsers.filter((u) => u.role === role).length
  }));

  const addUserMutation = useMutation({
    mutationFn: (values: AddAIHubUserFormValues) => {
      if (selectedAIHubUserIDs.length === 0) {
        throw new Error("请至少选择一个 AIHub 用户");
      }
      if (roleRequiresTeam(values.app_role) && !values.team_id) {
        throw new Error(teams.length === 0 ? "请先创建小组" : "employee/team_leader 必须选择小组");
      }
      return adminBatchAddUsers({
        user_ids: selectedAIHubUserIDs,
        app_role: values.app_role,
        team_id: roleRequiresTeam(values.app_role) ? values.team_id || undefined : undefined,
        local_enabled: values.local_enabled !== false
      });
    },
    onSuccess: async (result) => {
      const parts = [`新增 ${result.created} 人`];
      const skipped = result.skipped_existing ?? result.skipped;
      if (skipped > 0) parts.push(`跳过 ${skipped} 人`);
      if (result.failed > 0) parts.push(`失败 ${result.failed} 人`);
      void message.success(parts.join("，"));
      setAddUserOpen(false);
      setAddUserError(undefined);
      setAddUserSearch("");
      setSelectedAIHubUserIDs([]);
      addUserForm.resetFields();
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["users"] }),
        queryClient.invalidateQueries({ queryKey: ["teams"] }),
        queryClient.invalidateQueries({ queryKey: ["aihub-users"] })
      ]);
    },
    onError: (error) => setAddUserError(getApiErrorMessage(error, "添加 Aida 用户失败"))
  });

  const saveTeamMutation = useMutation({
    mutationFn: (values: CreateTeamFormValues) =>
      editingTeam
        ? adminUpdateTeam(editingTeam.id, {
            name: values.name.trim(),
            director_user_id: values.director_user_id || undefined
          })
        : adminCreateTeam({
            name: values.name.trim(),
            director_user_id: values.director_user_id || undefined
          }),
    onSuccess: async () => {
      setCreateTeamOpen(false);
      setEditingTeam(null);
      setCreateTeamError(undefined);
      createTeamForm.resetFields();
      await queryClient.invalidateQueries({ queryKey: ["teams"] });
    },
    onError: (error) => setCreateTeamError(getApiErrorMessage(error, "保存小组失败，请稍后重试"))
  });
  const saveDepartmentMutation = useMutation({
    mutationFn: (values: DepartmentFormValues) => editingDepartment ? adminUpdateDepartment(editingDepartment.id, values) : adminCreateDepartment(values),
    onSuccess: async () => { setDepartmentOpen(false); setEditingDepartment(null); departmentForm.resetFields(); await Promise.all([queryClient.invalidateQueries({queryKey:["departments"]}), queryClient.invalidateQueries({queryKey:["teams"]}), queryClient.invalidateQueries({queryKey:["users"]})]); },
    onError: (error) => message.error(getApiErrorMessage(error, "保存部门失败"))
  });

  const deleteTeamMutation = useMutation({
    mutationFn: (teamID: string) => adminDeleteTeam(teamID),
    onSuccess: async () => {
      void message.success("小组已删除");
      await queryClient.invalidateQueries({ queryKey: ["teams"] });
    },
    onError: (error) => message.error(getApiErrorMessage(error, "删除小组失败"))
  });

  const updateUserMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Parameters<typeof adminUpdateUser>[1] }) =>
      adminUpdateUser(id, data),
    onSuccess: async () => {
      void message.success("Aida 用户配置已更新");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["users"] }),
        queryClient.invalidateQueries({ queryKey: ["teams"] })
      ]);
    },
    onError: (error) => message.error(getApiErrorMessage(error, "用户配置更新失败"))
  });

  const submitRoleTeamChange = async (values: RoleTeamChangeFormValues) => {
    if (!roleTeamChange) return;
    try {
      await updateUserMutation.mutateAsync({
        id: roleTeamChange.user.id,
        data: {
          app_role: roleTeamChange.appRole,
          team_id: values.team_id
        }
      });
      setRoleTeamChange(null);
      roleTeamForm.resetFields();
    } catch {
      // Error is shown by updateUserMutation.onError.
    }
  };

  const handleRoleChange = (record: User, appRole: UserRole) => {
    const currentRole = record.app_role ?? record.role;
    if (appRole === currentRole) return;
    if (roleRequiresTeam(appRole)) {
      if (record.team_id) {
        updateUserMutation.mutate({
          id: record.id,
          data: { app_role: appRole, team_id: record.team_id }
        });
        return;
      }
      if (teams.length === 0) {
        void message.warning("请先创建小组，再设置工程师或组长角色");
        return;
      }
      setRoleTeamChange({ user: record, appRole });
      roleTeamForm.resetFields();
      return;
    }
    updateUserMutation.mutate({
      id: record.id,
      data: { app_role: appRole, clear_team: true }
    });
  };

  const aihubColumns: TableProps<AIHubUser>["columns"] = [
    {
      title: "nickname",
      dataIndex: "nickname",
      render: (value: string, record) => value || displayAIHubUser(record)
    },
    {
      title: "username",
      dataIndex: "username",
      render: (value: string) => value || "-"
    },
    {
      title: "email",
      dataIndex: "email",
      render: (value: string) => value || "-"
    },
    {
      title: "当前 Aida 状态",
      dataIndex: "aida_status",
      width: 140,
      render: (_: AIHubUser["aida_status"], record) => (
        <span className={`org-aida-status ${aidaStatusClass(record.aida_status)}`}>{aidaStatusLabel(record)}</span>
      )
    }
  ];

  const columns: TableProps<User>["columns"] = [
    {
      title: "用户",
      dataIndex: "nickname",
      render: (_: string, record) => (
        <span className={`org-name-cell${record.local_enabled === false ? " is-deactivated" : ""}`}>
          <span className={`org-name-cell__dot is-${record.role}`} aria-hidden="true" />
          <span>
            <strong>{displayUser(record)}</strong>
            <em>{record.username || record.employee_id || record.email || record.id}</em>
          </span>
        </span>
      )
    },
    {
      title: "username",
      dataIndex: "username",
      width: 150,
      render: (value: string) => value || "-"
    },
    {
      title: "邮箱",
      dataIndex: "email",
      render: (value: string) => <span style={{ color: "#7a879a" }}>{value || "-"}</span>
    },
    {
      title: "Aida 角色",
      dataIndex: "role",
      width: 170,
      render: (_: UserRole, record) => (
        <Select
          size="small"
          value={record.app_role ?? record.role}
          options={ROLE_OPTIONS}
          disabled={updateUserMutation.isPending || record.id === currentUser?.id}
          onChange={(appRole) => handleRoleChange(record, appRole)}
        />
      )
    },
    {
      title: "部门",
      key: "department",
      width: 160,
      render: (_: unknown, record) => {
        const department = departments.find((item) =>
          item.director_user_id === record.id ||
          item.pm_user_ids.includes(record.id) ||
          Boolean(record.team_id && item.team_ids.includes(record.team_id))
        );
        return department?.name || "未配置";
      }
    },
    {
      title: "Aida 小组",
      dataIndex: "team_id",
      width: 190,
      render: (_: string, record) => {
        const appRole = record.app_role ?? record.role;
        const needsTeam = roleRequiresTeam(appRole);
        const forcesNoTeam = roleForcesNoTeam(appRole);
        return (
          <Select
            size="small"
            value={forcesNoTeam ? "" : record.team_id ?? ""}
            options={[
              ...(needsTeam ? [] : [{ value: "", label: "未分组" }]),
              ...teams.map((team) => ({ value: team.id, label: team.name }))
            ]}
            disabled={updateUserMutation.isPending || forcesNoTeam}
            onChange={(teamID) => {
              if (needsTeam && !teamID) {
                void message.warning("工程师和组长必须选择小组");
                return;
              }
              updateUserMutation.mutate({
                id: record.id,
                data: teamID ? { team_id: teamID } : { clear_team: true }
              });
            }}
          />
        );
      }
    },
    {
      title: "Aida 访问",
      dataIndex: "local_enabled",
      width: 120,
      render: (value: boolean | undefined, record) => (
        <Switch
          checked={value !== false}
          checkedChildren="允许"
          unCheckedChildren="关闭"
          disabled={updateUserMutation.isPending || record.id === currentUser?.id}
          onChange={(checked) =>
            updateUserMutation.mutate({ id: record.id, data: { local_enabled: checked } })
          }
        />
      )
    },
    {
      title: "同步时间",
      dataIndex: "last_synced_at",
      width: 190,
      render: (value?: string | null) => (value ? new Date(value).toLocaleString() : "-")
    }
  ];

  if (!isAdmin) {
    return (
      <PagePanel title="组织" description="Aida 用户业务配置与小组管理" className="org-page" showNav={false}>
        <div className="org-empty-frame">
          <Empty description="仅管理员可管理 Aida 业务角色、小组和访问权限。" />
        </div>
      </PagePanel>
    );
  }

  return (
    <>
      <PagePanel
        title="组织"
        description="aihub 用户列表与 Aida 业务配置"
        className="org-page aidashboard-list"
        breadcrumbs={[{ title: "组织" }]}
        showNav={false}
      >
        <div className="org-page-actions">
          <Button
            icon={<PlusOutlined />}
            type="primary"
            onClick={() => {
              setAddUserOpen(true);
              setAIHubUsersPage(1);
              setSelectedAIHubUserIDs([]);
              addUserForm.setFieldsValue({ app_role: "employee", local_enabled: true });
            }}
          >
            从 AIHub 添加用户
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={usersQuery.isFetching || teamsQuery.isFetching}
            onClick={() => {
              void usersQuery.refetch();
              void teamsQuery.refetch();
            }}
          >
            刷新
          </Button>
        </div>
        <RequirementMetricGrid>
          {roleCounts.map(({ role, count }) => (
            <RequirementMetricCard
              key={role}
              tone={ROLE_TONE[role]}
              icon={ROLE_ICON[role]}
              loading={usersQuery.isLoading}
              metric={{
                key: role,
                title: ROLE_LABELS[role],
                value: count,
                description: "Aida 本地业务角色"
              }}
            />
          ))}
        </RequirementMetricGrid>

        <section className="org-team-section">
          <div className="org-team-section__head"><div className="org-section-title"><strong>部门</strong><span>{departments.length} 个部门</span></div>
            <Button size="small" icon={<PlusOutlined/>} onClick={() => { setEditingDepartment(null); departmentForm.setFieldsValue({team_ids:[],pm_user_ids:[]}); setDepartmentOpen(true); }}>添加部门</Button>
          </div>
          <div className="org-team-grid">{departments.map(department => <article className="org-team-card" key={department.id}>
            <header className="org-team-card__head"><span className="org-team-card__name">{department.name}</span><span>{department.team_ids.length} 个小组</span></header>
            <div className="org-team-card__row"><span className="org-team-card__row-label">总监</span><span>{department.director_name || "未配置"}</span></div>
            <div className="org-team-card__row"><span className="org-team-card__row-label">直属 PM</span><span>{department.pm_user_ids.length} 人</span></div>
            <div className="org-team-card__actions"><Button size="small" icon={<EditOutlined/>} onClick={() => {setEditingDepartment(department); departmentForm.setFieldsValue({name:department.name,director_user_id:department.director_user_id || undefined,team_ids:department.team_ids,pm_user_ids:department.pm_user_ids}); setDepartmentOpen(true);}}>批量配置</Button></div>
          </article>)}</div>
        </section>

        <section className="org-team-section">
          <div className="org-team-section__head">
            <div className="org-section-title">
              <strong>小组</strong>
              <span>
                {teams.length} 个小组 · {enabledUsers.length} 名允许访问用户
              </span>
            </div>
            <Button
              size="small"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditingTeam(null);
                createTeamForm.resetFields();
                setCreateTeamOpen(true);
              }}
            >
              添加小组
            </Button>
          </div>
          {teamsQuery.isLoading ? (
            <Empty description="加载中" />
          ) : teams.length === 0 ? (
            <Empty description="暂无小组" />
          ) : (
            <div className="org-team-grid">
              {teams.map((team) => {
                const members = teamMembers(team.id);
                const directors = team.director_name ? [team.director_name] : [];
                const leaders = members.filter((u) => u.role === "team_leader");
                const visibleMembers = members.slice(0, 5);
                const hidden = members.length - visibleMembers.length;
                return (
                  <article className="org-team-card" key={team.id}>
                    <header className="org-team-card__head">
                      <span className="org-team-card__name">{team.name}</span>
                      <span className="org-team-card__count">
                        <strong>{members.length}</strong> 成员
                      </span>
                    </header>
                    <div className="org-team-card__row">
                      <span className="org-team-card__row-label">所属总监</span>
                      <span>{directors.length ? directors.join("、") : "未配置"}</span>
                    </div>
                    <div className="org-team-card__row">
                      <span className="org-team-card__row-label">TL</span>
                      <span>{leaders.length ? leaders.map(displayUser).join("、") : "未分配"}</span>
                    </div>
                    <div className="org-team-card__row">
                      <span className="org-team-card__row-label">人员</span>
                      {members.length ? (
                        <span className="org-team-card__avatars">
                          {visibleMembers.map((member) => (
                            <span
                              key={member.id}
                              className={`org-team-card__avatar ${avatarToneClass(member.role)}`}
                              title={`${displayUserWithUsername(member)} (${ROLE_LABELS[member.role]})`}
                            >
                              {initials(displayUser(member))}
                            </span>
                          ))}
                          {hidden > 0 ? <span className="org-team-card__more">+{hidden}</span> : null}
                        </span>
                      ) : (
                        <span className="org-team-card__empty">暂无成员</span>
                      )}
                    </div>
                    <div className="org-team-card__actions">
                      <Button
                        size="small"
                        icon={<EditOutlined />}
                        onClick={() => {
                          setEditingTeam(team);
                          setCreateTeamError(undefined);
                          createTeamForm.setFieldsValue({
                            name: team.name,
                            director_user_id: team.director_user_id || undefined
                          });
                          setCreateTeamOpen(true);
                        }}
                      >
                        编辑
                      </Button>
                      <Button
                        size="small"
                        danger
                        icon={<DeleteOutlined />}
                        loading={deleteTeamMutation.isPending}
                        onClick={() => {
                          Modal.confirm({
                            title: "删除小组",
                            content: `确认删除「${team.name}」？该小组已有成员或业务数据时不能删除。`,
                            okText: "删除",
                            okButtonProps: { danger: true },
                            cancelText: "取消",
                            onOk: () => deleteTeamMutation.mutateAsync(team.id)
                          });
                        }}
                      >
                        删除
                      </Button>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
        </section>

        <TableLayout>
          <div className="org-user-search"><Input.Search allowClear placeholder="搜索姓名、username、工号或邮箱" value={userSearch} onChange={event => setUserSearch(event.target.value)} /></div>
          {usersQuery.isError ? (
            <Alert
              type="error"
              showIcon
              message="用户列表加载失败"
              description={usersQuery.error instanceof Error ? usersQuery.error.message : "请稍后重试"}
              action={<Button onClick={() => void usersQuery.refetch()}>重试</Button>}
            />
          ) : null}
          <ResourceTable<User>
            rowKey="id"
            columns={columns}
            dataSource={visibleUsers}
            loading={usersQuery.isLoading}
            pagination={{ pageSize: 20, showSizeChanger: false }}
          />
        </TableLayout>
      </PagePanel>

      <Modal
        className="org-form-modal"
        title="选择 Aida 小组"
        open={Boolean(roleTeamChange)}
        confirmLoading={updateUserMutation.isPending}
        okText="确认修改"
        cancelText="取消"
        onCancel={() => {
          if (updateUserMutation.isPending) return;
          setRoleTeamChange(null);
          roleTeamForm.resetFields();
        }}
        onOk={() => roleTeamForm.submit()}
        destroyOnHidden
      >
        {roleTeamChange ? (
          <>
            <Alert
              type="info"
              showIcon
              message={`将 ${displayUser(roleTeamChange.user)} 设置为 ${ROLE_LABELS[roleTeamChange.appRole]}，需要同时选择所属小组。`}
              className="org-modal-alert"
            />
            <Form
              form={roleTeamForm}
              layout="vertical"
              requiredMark={false}
              onFinish={(values) => void submitRoleTeamChange(values)}
            >
              <Form.Item label="Aida 小组" name="team_id" rules={[{ required: true, message: "请选择小组" }]}>
                <Select
                  placeholder="请选择小组"
                  options={teams.map((team) => ({ value: team.id, label: team.name }))}
                />
              </Form.Item>
            </Form>
          </>
        ) : null}
      </Modal>

      <Modal
        className="org-form-modal org-add-user-modal"
        title="从 AIHub 添加用户"
        open={addUserOpen}
        confirmLoading={addUserMutation.isPending}
        okButtonProps={{ disabled: selectedAIHubUserIDs.length === 0 }}
        okText="添加"
        cancelText="取消"
        width={860}
        onCancel={() => {
          setAddUserOpen(false);
          setAddUserError(undefined);
          setAddUserSearch("");
          setAIHubUsersPage(1);
          setSelectedAIHubUserIDs([]);
          addUserForm.resetFields();
        }}
        onOk={() => addUserForm.submit()}
        destroyOnHidden
      >
        {addUserError ? <Alert type="error" showIcon message={addUserError} className="org-modal-alert" /> : null}
        <div className="org-add-user-search">
          <Input.Search
            allowClear
            placeholder="按 username / nickname / email 搜索 AIHub 用户"
            loading={aihubUsersQuery.isFetching}
            onSearch={(value) => {
              setAddUserSearch(value);
              setAIHubUsersPage(1);
            }}
            onChange={(event) => {
              if (!event.target.value) {
                setAddUserSearch("");
                setAIHubUsersPage(1);
              }
            }}
          />
        </div>
        <Table<AIHubUser>
          rowKey="id"
          size="small"
          columns={aihubColumns}
          dataSource={aihubUsers}
          loading={aihubUsersQuery.isFetching}
          pagination={{
            current: aihubUsersQuery.data?.page_num ?? aihubUsersPage,
            pageSize: aihubUsersQuery.data?.page_size ?? aihubUsersPageSize,
            total: aihubUsersQuery.data?.total ?? 0,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 个 AIHub 用户`
          }}
          onChange={(pagination) => {
            setAIHubUsersPage(pagination.current ?? 1);
            setAIHubUsersPageSize(pagination.pageSize ?? 10);
          }}
          rowSelection={{
            selectedRowKeys: selectedAIHubUserIDs,
            getCheckboxProps: (record) => ({
              disabled: record.aida_status === "active" || record.aida_status === "disabled"
            }),
            onChange: (keys) => {
              setSelectedAIHubUserIDs(keys.map((key) => Number(key)).filter((key) => Number.isFinite(key)));
              setAddUserError(undefined);
            }
          }}
        />
        <Form
          form={addUserForm}
          layout="vertical"
          requiredMark={false}
          initialValues={{ app_role: "employee", local_enabled: true }}
          onFinish={(values) => void addUserMutation.mutateAsync(values)}
          onValuesChange={() => setAddUserError(undefined)}
        >
          <div className="org-add-user-config">
            <div className="org-add-user-config__summary">
              已选择 <strong>{selectedAIHubUserIDs.length}</strong> 个 AIHub 用户
              {selectedAIHubUsers.length ? `：${selectedAIHubUsers.map(displayAIHubUserWithUsername).join("、")}` : null}
            </div>
            {selectedAIHubUserIDs.length > 0 ? (
              <>
                <Form.Item label="Aida 角色" name="app_role" rules={[{ required: true, message: "请选择 Aida 角色" }]}>
                  <Select
                    options={ROLE_OPTIONS}
                    onChange={(role: UserRole) => {
                      if (!roleRequiresTeam(role)) {
                        addUserForm.setFieldsValue({ team_id: undefined });
                      }
                    }}
                  />
                </Form.Item>
                <Form.Item noStyle shouldUpdate={(prev, next) => prev.app_role !== next.app_role}>
                  {({ getFieldValue }) => {
                    const appRole = getFieldValue("app_role") as UserRole | undefined;
                    const needsTeam = roleRequiresTeam(appRole);
                    return (
                      <>
                        {!needsTeam ? (
                          <Alert
                            type="info"
                            showIcon
                            message="admin、director、pm 为跨组角色，小组将由后端自动清空。"
                            className="org-modal-alert"
                          />
                        ) : null}
                        {needsTeam && teams.length === 0 ? (
                          <Alert
                            type="warning"
                            showIcon
                            message="请先创建小组，再添加 employee 或 team_leader。"
                            className="org-modal-alert"
                          />
                        ) : null}
                        <Form.Item
                          label="Aida 小组"
                          name="team_id"
                          rules={needsTeam ? [{ required: true, message: "employee/team_leader 必须选择小组" }] : undefined}
                        >
                          <Select
                            allowClear
                            disabled={!needsTeam}
                            placeholder={needsTeam ? "请选择小组" : "跨组角色无需选择小组"}
                            options={teams.map((team) => ({ value: team.id, label: team.name }))}
                          />
                        </Form.Item>
                      </>
                    );
                  }}
                </Form.Item>
                <Form.Item label="Aida 访问" name="local_enabled" valuePropName="checked">
                  <Switch checkedChildren="允许" unCheckedChildren="关闭" />
                </Form.Item>
              </>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请先从上方搜索结果中选择用户" />
            )}
          </div>
        </Form>
      </Modal>

      <Modal
        className="org-form-modal"
        title={editingTeam ? "编辑小组" : "添加小组"}
        open={createTeamOpen}
        confirmLoading={saveTeamMutation.isPending}
        okText={editingTeam ? "保存" : "创建"}
        cancelText="取消"
        onCancel={() => {
          setCreateTeamOpen(false);
          setEditingTeam(null);
          setCreateTeamError(undefined);
          createTeamForm.resetFields();
        }}
        onOk={() => createTeamForm.submit()}
        destroyOnHidden
      >
        {createTeamError ? (
          <Alert type="error" showIcon message={createTeamError} className="org-modal-alert" />
        ) : null}
        <Form
          form={createTeamForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => void saveTeamMutation.mutateAsync(values)}
          onValuesChange={() => setCreateTeamError(undefined)}
        >
          <Form.Item label="小组名称" name="name" rules={[{ required: true, message: "请输入小组名称" }]}>
            <Input placeholder="请输入小组名称" />
          </Form.Item>
          <Form.Item
            label="所属总监"
            name="director_user_id"
            extra="这是小组上级归属，不是团队负责人 TL。"
          >
            <Select
              allowClear
              placeholder="选择总监"
              options={users
                .filter((user) => user.role === "director" && user.local_enabled !== false)
                .map((user) => ({ value: user.id, label: displayUserWithUsername(user) }))}
            />
          </Form.Item>
        </Form>
      </Modal>
      <Modal className="org-form-modal" width={760} title={editingDepartment ? "批量配置部门" : "添加部门"} open={departmentOpen}
        confirmLoading={saveDepartmentMutation.isPending} okText="保存" cancelText="取消"
        onCancel={() => {setDepartmentOpen(false); setEditingDepartment(null); departmentForm.resetFields();}}
        onOk={() => departmentForm.submit()} destroyOnHidden>
        <Form form={departmentForm} layout="vertical" requiredMark={false} onFinish={values => saveDepartmentMutation.mutate(values)}>
          <Form.Item label="部门名称" name="name" rules={[{required:true,message:"请输入部门名称"}]}><Input/></Form.Item>
          <Form.Item label="部门总监" name="director_user_id"><Select allowClear showSearch optionFilterProp="label" options={users.filter(u=>u.role==="director").map(u=>({value:u.id,label:displayUserWithUsername(u)}))}/></Form.Item>
          <Form.Item label="包含小组" name="team_ids"><Select mode="multiple" showSearch optionFilterProp="label" options={teams.map(t=>({value:t.id,label:t.name}))}/></Form.Item>
          <Form.Item label="直属 PM" name="pm_user_ids"><Select mode="multiple" showSearch optionFilterProp="label" options={users.filter(u=>u.role==="pm").map(u=>({value:u.id,label:displayUserWithUsername(u)}))}/></Form.Item>
        </Form>
      </Modal>
    </>
  );
}
