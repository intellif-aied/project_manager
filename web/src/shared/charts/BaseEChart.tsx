import { Alert, Empty } from "antd";
import * as echarts from "echarts";
import type { ECharts, EChartsOption, SetOptionOpts } from "echarts";
import { useEffect, useRef } from "react";

import "./BaseEChart.css";

interface BaseEChartProps {
  option: EChartsOption;
  height?: number;
  className?: string;
  loading?: boolean;
  empty?: boolean;
  error?: boolean;
  errorMessage?: string;
  renderer?: "canvas" | "svg";
  setOptionOpts?: SetOptionOpts;
}

export function BaseEChart({
  option,
  height = 340,
  className,
  loading = false,
  empty = false,
  error = false,
  errorMessage = "图表加载失败",
  renderer = "canvas",
  setOptionOpts
}: BaseEChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<ECharts>();
  const frameRef = useRef<number>();

  useEffect(() => {
    if (!containerRef.current || empty || error) return undefined;
    chartRef.current = echarts.init(containerRef.current, undefined, { renderer });

    return () => {
      if (frameRef.current) window.cancelAnimationFrame(frameRef.current);
      chartRef.current?.dispose();
      chartRef.current = undefined;
    };
  }, [empty, error, renderer]);

  useEffect(() => {
    if (!chartRef.current || empty || error) return;
    chartRef.current.setOption(option, setOptionOpts);
  }, [empty, error, option, setOptionOpts]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    if (loading) chart.showLoading("default", { text: "加载中..." });
    else chart.hideLoading();
  }, [loading]);

  useEffect(() => {
    if (!containerRef.current) return undefined;
    const resizeObserver = new ResizeObserver(() => {
      if (frameRef.current) window.cancelAnimationFrame(frameRef.current);
      frameRef.current = window.requestAnimationFrame(() => {
        chartRef.current?.resize();
      });
    });
    resizeObserver.observe(containerRef.current);

    return () => resizeObserver.disconnect();
  }, []);

  return (
    <div className={`base-echart${className ? ` ${className}` : ""}`} style={{ height }}>
      <div
        ref={containerRef}
        className="base-echart__canvas"
        style={{ display: empty || error ? "none" : undefined, height }}
      />
      {error ? (
        <div className="base-echart__state" style={{ height }}>
          <Alert type="error" showIcon message={errorMessage} />
        </div>
      ) : null}
      {empty && !error ? (
        <div className="base-echart__state" style={{ height }}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无图表数据" />
        </div>
      ) : null}
    </div>
  );
}
