import {
  ArrowLeftOutlined,
  CalendarOutlined,
  CheckOutlined,
  CloudUploadOutlined,
  CopyOutlined,
  DashboardOutlined,
  FileTextOutlined,
  PictureOutlined,
  ProjectOutlined,
  RightOutlined,
  RocketOutlined,
  SearchOutlined
} from "@ant-design/icons";
import { Button, Drawer, Empty, Image, Input, Tag } from "antd";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { useAuth } from "@/shared/auth/authContext";

import {
  HELP_ARTICLES,
  HELP_MODULE_LABELS,
  HELP_ROLE_LABELS,
  articleSupportsRole,
  helpArticleSearchText,
  isBusinessRole,
  sectionSupportsRole,
  type HelpArticle
} from "./helpCenterContent";
import type { HelpModuleKey } from "./helpCenterConfig";
import "./HelpCenter.css";

interface HelpCenterProps {
  onClose: () => void;
  open: boolean;
}

const HELP_MODULES = [
  { key: "quickstart", label: "快速开始", icon: <RocketOutlined /> },
  { key: "client", label: "AIDA 客户端", icon: <CloudUploadOutlined /> },
  { key: "workspace", label: "工作台", icon: <DashboardOutlined /> },
  { key: "requirements", label: "需求看板", icon: <ProjectOutlined /> },
  { key: "daily", label: "日报", icon: <FileTextOutlined /> },
  { key: "weekly", label: "周报", icon: <CalendarOutlined /> }
] as const;

async function copyText(text: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

function HelpArticleList({ articles, onOpen }: { articles: HelpArticle[]; onOpen: (id: string) => void }) {
  if (!articles.length) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配当前角色的帮助内容" />;
  }

  return (
    <div className="help-center__article-list">
      {articles.map((article) => (
        <button key={article.id} type="button" className="help-center__article-card" onClick={() => onOpen(article.id)}>
          <span>
            <strong>{article.title}</strong>
            <small>{article.summary}</small>
          </span>
          <RightOutlined />
        </button>
      ))}
    </div>
  );
}

export function HelpCenter({ onClose, open }: HelpCenterProps) {
  const navigate = useNavigate();
  const { user } = useAuth();
  const [selectedModule, setSelectedModule] = useState<HelpModuleKey>("quickstart");
  const [selectedArticleId, setSelectedArticleId] = useState<string>();
  const [searchDraft, setSearchDraft] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [copiedCode, setCopiedCode] = useState<string>();

  const role = isBusinessRole(user?.role) ? user.role : undefined;
  const allowedArticles = useMemo(
    () => (role ? HELP_ARTICLES.filter((article) => articleSupportsRole(article, role)) : []),
    [role]
  );
  const moduleArticles = useMemo(
    () => allowedArticles.filter((article) => article.module === selectedModule),
    [allowedArticles, selectedModule]
  );
  const searchResults = useMemo(() => {
    const keyword = searchQuery.trim().toLocaleLowerCase();
    if (!keyword) return [];
    return allowedArticles.filter((article) => helpArticleSearchText(article).includes(keyword));
  }, [allowedArticles, searchQuery]);
  const selectedArticle = allowedArticles.find((article) => article.id === selectedArticleId);

  if (!role) return null;

  const resetToModule = (module: HelpModuleKey) => {
    setSelectedModule(module);
    setSelectedArticleId(undefined);
    setSearchDraft("");
    setSearchQuery("");
  };

  const openArticle = (id: string) => {
    setSelectedArticleId(id);
    setSearchDraft("");
    setSearchQuery("");
  };

  const renderArticle = (article: HelpArticle) => (
    <article className="help-center__article-detail">
      <Button type="text" className="help-center__back" icon={<ArrowLeftOutlined />} onClick={() => setSelectedArticleId(undefined)}>
        返回{HELP_MODULE_LABELS[article.module]}
      </Button>
      <div className="help-center__article-head">
        <div className="help-center__article-eyebrow">
          <span>{HELP_MODULE_LABELS[article.module]}</span>
          <Tag bordered={false}>{HELP_ROLE_LABELS[role]}</Tag>
        </div>
        <h2>{article.title}</h2>
        <p>{article.summary}</p>
      </div>
      <div className="help-center__article-sections">
        {article.sections.filter((section) => sectionSupportsRole(section, role)).map((section, sectionIndex) => (
          <section key={`${article.id}-${section.title}-${sectionIndex}`}>
            <h3>{section.title}</h3>
            {section.paragraphs?.map((paragraph) => <p key={paragraph}>{paragraph}</p>)}
            {section.steps ? (
              <ol>{section.steps.map((step) => <li key={step}>{step}</li>)}</ol>
            ) : null}
            {section.bullets ? (
              <ul>{section.bullets.map((bullet) => <li key={bullet}>{bullet}</li>)}</ul>
            ) : null}
            {section.codeBlocks?.map((block) => {
              const isCopied = copiedCode === block.code;
              return (
                <div key={`${block.label}-${block.code}`} className="help-center__code-block">
                  <div>
                    <span>{block.label}</span>
                    <Button
                      aria-label={`复制${block.label}`}
                      icon={isCopied ? <CheckOutlined /> : <CopyOutlined />}
                      size="small"
                      type="text"
                      onClick={() => {
                        void copyText(block.code).then(() => {
                          setCopiedCode(block.code);
                          window.setTimeout(() => setCopiedCode((current) => current === block.code ? undefined : current), 1600);
                        });
                      }}
                    >
                      {isCopied ? "已复制" : "复制"}
                    </Button>
                  </div>
                  <code>{block.code}</code>
                </div>
              );
            })}
            {section.note ? <div className="help-center__note">{section.note}</div> : null}
            {section.screenshots?.filter((screenshot) => !screenshot.roles || screenshot.roles.includes(role)).map((screenshot) => (
              <figure key={screenshot.src} className="help-center__figure">
                <Image src={screenshot.src} alt={screenshot.alt} preview={{ mask: "查看原图" }} />
                <figcaption>{screenshot.caption}</figcaption>
              </figure>
            ))}
            {section.screenshotPlaceholders?.map((placeholder) => (
              <div key={placeholder.title} className="help-center__screenshot-placeholder">
                <PictureOutlined />
                <span>
                  <strong>{placeholder.title}</strong>
                  <small>{placeholder.description}</small>
                </span>
                <Tag bordered={false}>等待截图</Tag>
              </div>
            ))}
          </section>
        ))}
      </div>
      <div className="help-center__article-footer">
        <Button
          className="help-center__article-link"
          icon={<RightOutlined />}
          iconPosition="end"
          onClick={() => {
            navigate(article.route);
            onClose();
          }}
        >
          前往对应页面
        </Button>
        <span>最后核对：2026-07-14</span>
      </div>
    </article>
  );

  return (
    <Drawer
      className="help-center"
      destroyOnHidden
      open={open}
      placement="right"
      size="min(1040px, 100vw)"
      title={
        <div className="help-center__title">
          <strong>AIDA 指南中心</strong>
          <span>{HELP_ROLE_LABELS[role]}</span>
        </div>
      }
      onClose={onClose}
    >
      <div className="help-center__search">
        <Input.Search
          allowClear
          enterButton={<SearchOutlined />}
          placeholder="搜索操作、字段或问题"
          value={searchDraft}
          onChange={(event) => {
            const value = event.target.value;
            setSearchDraft(value);
            if (!value) setSearchQuery("");
          }}
          onSearch={(value) => {
            setSearchQuery(value);
            setSelectedArticleId(undefined);
          }}
        />
      </div>

      <div className="help-center__body">
        <aside className="help-center__sidebar" aria-label="帮助模块">
          <nav className="help-center__nav">
            {HELP_MODULES.map((item) => {
              const count = allowedArticles.filter((article) => article.module === item.key).length;
              return (
                <button
                  key={item.key}
                  className={!searchQuery && item.key === selectedModule ? "is-active" : ""}
                  type="button"
                  onClick={() => resetToModule(item.key)}
                >
                  {item.icon}
                  <span>{item.label}</span>
                  <small>{count}</small>
                </button>
              );
            })}
          </nav>
          <div className="help-center__role-note">
            <span>当前身份</span>
            <strong>{HELP_ROLE_LABELS[role]}</strong>
            <small>仅显示当前角色可执行的流程</small>
          </div>
        </aside>

        <main className="help-center__main" aria-live="polite">
          {selectedArticle ? renderArticle(selectedArticle) : searchQuery ? (
            <section className="help-center__index">
              <div className="help-center__index-head">
                <span>搜索结果</span>
                <h2>“{searchQuery}”</h2>
                <p>找到 {searchResults.length} 篇与当前角色相关的指南。</p>
              </div>
              <HelpArticleList articles={searchResults} onOpen={openArticle} />
            </section>
          ) : (
            <section className="help-center__index">
              <div className="help-center__index-head">
                <span>模块指南</span>
                <h2>{HELP_MODULE_LABELS[selectedModule]}</h2>
                <p>选择一个任务，查看完整操作步骤、权限说明和常见风险。</p>
              </div>
              <HelpArticleList articles={moduleArticles} onOpen={openArticle} />
            </section>
          )}
        </main>
      </div>
    </Drawer>
  );
}
