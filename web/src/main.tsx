import "antd/dist/reset.css";
import "./shared/styles/global.css";
import "dayjs/locale/zh-cn";

import React from "react";
import ReactDOM from "react-dom/client";
import dayjs from "dayjs";

import { AppProviders } from "@/app/providers";
import { runtimeConfig } from "@/config/runtimeConfig";

dayjs.locale("zh-cn");
document.title = runtimeConfig.appTitle;

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <AppProviders />
  </React.StrictMode>
);
