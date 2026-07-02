import {
  ApiOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  LockOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Spin } from "antd";
import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";

import { runtimeConfig } from "@/config/runtimeConfig";
import { useAuth } from "@/shared/auth/authContext";

import "./LoginPage.css";

const DASHBOARD_PATH = "/dashboard";

const accessSignals = [
  { label: "身份验证", status: "已加密", tone: "cyan" },
  { label: "权限校验", status: "已校验", tone: "amber" },
  { label: "安全连接", status: "内部专线", tone: "green" },
];

const operationLines = [
  "已建立安全连接",
  "已加载身份校验策略",
  "已完成访问范围校验",
  "工作台环境准备就绪",
];

export function LoginPage() {
  const navigate = useNavigate();
  const { status, isAuthenticated, login } = useAuth();
  const [submitting, setSubmitting] = useState(false);
  const [loginError, setLoginError] = useState<string | null>(null);

  if (isAuthenticated) {
    return <Navigate to={DASHBOARD_PATH} replace />;
  }

  return (
    <main className="login-page">
      <div className="login-page__background" aria-hidden="true">
        <span className="login-page__beam login-page__beam--one" />
        <span className="login-page__beam login-page__beam--two" />
        <span className="login-page__mesh" />
      </div>

      <section className="login-page__shell login-page__shell--login" aria-labelledby="login-title">
        <div className="login-page__intro">
          <div className="login-page__brand-row">
            <img className="login-page__brand-logo" src="/logo.svg" alt="" aria-hidden="true" />
            <span>
              <strong>{runtimeConfig.appTitle}</strong>
              <em>内部协作控制台</em>
            </span>
          </div>

          <div className="login-page__hero">
            <p className="login-page__eyebrow">Aida 内部工作台</p>
            <h1>进入 Aida 内部工作台</h1>
            <p>
              使用授权账号访问团队协作环境，保持入口清晰、安全、专注。
            </p>
          </div>

          <div className="login-page__signal-grid" aria-label="访问状态">
            {accessSignals.map((item) => (
              <article key={item.label} className={`login-page__signal login-page__signal--${item.tone}`}>
                <span>{item.label}</span>
                <strong>{item.status}</strong>
              </article>
            ))}
          </div>

          <div className="login-page__terminal" aria-label="认证流程">
            <div className="login-page__terminal-head">
              <span />
              <span />
              <span />
            </div>
            <div className="login-page__terminal-body">
              {operationLines.map((line, index) => (
                <p key={line} style={{ animationDelay: `${index * 120}ms` }}>
                  <CheckCircleOutlined />
                  <span>{line}</span>
                </p>
              ))}
            </div>
          </div>
        </div>

        <Card className="login-page__card">
          <div className="login-page__access-strip" aria-hidden="true">
            <span className="is-active" />
            <span />
            <span />
            <span />
          </div>

          <div className="login-page__title">
            <span className="login-page__secure-badge">
              <SafetyCertificateOutlined />
              内部系统
            </span>
            <h2 id="login-title">登录工作台</h2>
            <p>使用统一平台账号进入 Aida 控制台。</p>
          </div>

          <Form
            layout="vertical"
            requiredMark={false}
            initialValues={
              import.meta.env.DEV
                ? { username: "admin", password: "123" }
                : undefined
            }
            onValuesChange={() => setLoginError(null)}
            onFinish={async (values: { username: string; password: string }) => {
              setSubmitting(true);
              setLoginError(null);
              try {
                await login({
                  username: values.username.trim(),
                  password: values.password,
                });
                navigate(DASHBOARD_PATH, { replace: true });
              } catch (error) {
                setLoginError(error instanceof Error ? error.message : "登录失败，请稍后重试");
              } finally {
                setSubmitting(false);
              }
            }}
          >
            <Form.Item
              label="工号"
              name="username"
              rules={[{ required: true, message: "请输入工号或登录名" }]}
            >
              <Input
                prefix={<UserOutlined />}
                autoComplete="username"
                placeholder="例如 admin"
                size="large"
              />
            </Form.Item>
            <Form.Item
              label="密码"
              name="password"
              rules={[
                { required: true, message: "请输入密码" },
                { min: 3, message: "密码至少 3 位" }
              ]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                autoComplete="current-password"
                placeholder="请输入密码"
                size="large"
              />
            </Form.Item>

            {loginError ? (
              <Alert type="error" showIcon message={loginError} className="login-page__error" />
            ) : null}

            <Button type="primary" htmlType="submit" block loading={submitting} size="large">
              登录
            </Button>
          </Form>

          <div className="login-page__card-footer">
            <span>
              <ApiOutlined />
              私有访问
            </span>
            <span>
              <ClockCircleOutlined />
              安全连接
            </span>
          </div>

          {status === "initializing" && !submitting ? (
            <div className="login-page__session-loading">
              <Spin size="small" />
              <span>正在恢复登录状态...</span>
            </div>
          ) : null}
        </Card>
      </section>
    </main>
  );
}
