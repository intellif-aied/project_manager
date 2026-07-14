import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Empty, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { LeftOutlined, RightOutlined } from "@ant-design/icons";

import type { MemberPersonalReport } from "../api/types";
import { MarkdownViewer } from "@/shared/components/MarkdownViewer/MarkdownViewer";

const roleLabels: Record<string, string> = {
  director: "总监", pm: "PM", team_leader: "TL", employee: "员工", admin: "管理员"
};

export function MemberReportBrowser<T extends { content: string }>({
  items, loading, error, queryKey, fetchDetail
}: {
  items: MemberPersonalReport[];
  loading: boolean;
  error?: string;
  queryKey: string;
  fetchDetail: (id: string) => Promise<T>;
}) {
  const [teamID, setTeamID] = useState("all");
  const [selectedID, setSelectedID] = useState<string>();
  const teams = useMemo(() => Array.from(new Map(items.filter(x => x.team_id).map(x => [x.team_id!, x.team_name])).entries()), [items]);
  const filtered = useMemo(() => teamID === "all" ? items : items.filter(x => x.team_id === teamID), [items, teamID]);
  const available = filtered.filter(x => x.report_id);

  const effectiveSelectedID = available.some(x => x.report_id === selectedID)
    ? selectedID
    : available[0]?.report_id;

  const detail = useQuery({
    queryKey: ["reports", "member-browser", queryKey, effectiveSelectedID],
    queryFn: () => fetchDetail(effectiveSelectedID!),
    enabled: Boolean(effectiveSelectedID),
    staleTime: 30_000
  });
  const currentIndex = available.findIndex(x => x.report_id === effectiveSelectedID);
  const columns: ColumnsType<MemberPersonalReport> = [
    { title: "成员", dataIndex: "user_name", width: 100, ellipsis: true },
    { title: "角色/小组", width: 120, render: (_, x) => <span>{roleLabels[x.role] ?? x.role}<br/><small>{x.team_name || "直属部门"}</small></span> },
    { title: "状态", width: 85, render: (_, x) => x.has_report ? <Tag color="green">已填写</Tag> : <Tag>未填写</Tag> },
    { title: "更新时间", dataIndex: "saved_at", width: 110, render: (v?: string) => v ? new Intl.DateTimeFormat("zh-CN", {month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hour12:false}).format(new Date(v)) : "-" },
    { title: "正文摘要", dataIndex: "content_preview", ellipsis: true, render: (v?: string) => v?.replace(/\s+/g, " ") || "-" }
  ];

  return <div className="member-report-browser">
    <section className="member-report-browser__list">
      <div className="member-report-browser__toolbar">
        <strong>成员报告</strong>
        {teams.length > 1 ? <Select value={teamID} onChange={setTeamID} options={[{value:"all",label:"全部小组"}, ...teams.map(([value,label]) => ({value,label}))]} /> : null}
      </div>
      {error ? <Alert type="error" showIcon message={error}/> : <Table rowKey="user_id" size="small" loading={loading} columns={columns} dataSource={filtered}
        pagination={{pageSize:10, showSizeChanger:true, pageSizeOptions:[10,20,50], showTotal:n=>`共 ${n} 人`}}
        onRow={record => ({className: record.report_id === effectiveSelectedID ? "is-selected" : "", onClick: () => record.report_id && setSelectedID(record.report_id)})}/>}
    </section>
    <section className="member-report-browser__detail">
      <header><strong>{available[currentIndex]?.user_name || "报告正文"}</strong><Space>
        <Button icon={<LeftOutlined/>} disabled={currentIndex <= 0} onClick={() => setSelectedID(available[currentIndex-1]?.report_id)}/>
        <Button icon={<RightOutlined/>} disabled={currentIndex < 0 || currentIndex >= available.length-1} onClick={() => setSelectedID(available[currentIndex+1]?.report_id)}/>
      </Space></header>
      <div className="member-report-browser__content">{detail.isLoading ? "正在加载..." : detail.isError ? <Alert type="error" message="报告加载失败"/> : detail.data?.content?.trim() ? <MarkdownViewer value={detail.data.content}/> : <Empty description="请选择已填写的报告"/>}</div>
    </section>
  </div>;
}
