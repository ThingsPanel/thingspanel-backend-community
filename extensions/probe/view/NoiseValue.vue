<template>
  <!--插件调试区域01-->
  <div class="chart-all-kA6iA2">
    <div class="chart-top-kA6iA2">
      <div style="
      text-align:center;
      color: #fff;
      width: 100%;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    ">噪音值</div>
      <!-- <div style="color: #5b92ff">Noise concentration</div> -->
    </div>
    <div class="chart-body-kA6iA2" :id="'chart_' + id"></div>
  </div>
</template>
<script>
  // import * as echarts from "echarts";
  export default {
    props: {
      id: {
        type: Number,
        default: 0,
      },
      loading: {
        type: Boolean,
        default: true,
      },
      apiData: {
        type: Object,
      },
      legend: {
        type: Boolean,
        default: true,
      },
    },
    data() {
      return {
        // 插件调试区域02
        latest: {},
        fields: [],
        chart: null,
        noise: 0,
      };
    },
    watch: {
      apiData: {
        //document.getElementById('ElementId').removeAttribute("style")
        //style="width: 100%; height: 100%"
        //apiData的数据样例，fields是传感器一段时间内的数据，latest是传感器最新数据
        // {
        //       "device_id": "f1ab2c47-951f-10b8-60c0-c6b33440f189",
        //       "fields": [
        //           {
        //               "hum": 24,
        //               "systime": "2022-01-18 18:59:11",
        //               "temp": 26
        //           },
        //           {
        //               "hum": 24,
        //               "systime": "2022-01-18 18:59:17",
        //               "temp": 26
        //           },
        //           {
        //               "hum": 24,
        //               "systime": "2022-01-18 18:59:41",
        //               "temp": 26
        //           }
        //       ],
        //       "latest": {
        //           "hum": 24,
        //           "systime": "2022-01-18 18:59:41",
        //           "temp": 26
        //       }
        //   }
        // deep: true,
        immediate: true,
        handler(val, oldVal) {
          console.log("01-kA6iA2-图表接收到数据");
          console.log("02-kA6iA2-图表id:" + this.id);
          if (val["fields"]) {
            console.log("03-kA6iA2-fields有值");
            console.log("04-kA6iA2-device_id:" + val["device_id"]);
            this.latest = val["latest"];
            this.fields = val["fields"];
            this.getData();
          } else {
            console.log("05-kA6iA2-fields没有值");
          }
        },
      },
      colorStart() { },
      colorEnd() { },
      legend(val, oldVal) {
        this.chart.setOption({
          legend: {
            show: val,
          },
        });
      },
    },
    // mounted() {
    //   this.initChart();
    //   const resizeObserver = new ResizeObserver((entries) => {
    //     this.chart && this.chart.resize();
    //   });
    //   resizeObserver.observe(document.getElementById("chart_" + this.id));
    // },
    methods: {
      // 插件调试区域03
      getData() {
        this.noise = this.latest.noise;
        setTimeout(() => {
          this.initChart();
        }, 1000);
      },
      initChart() {
        console.log("05-kA6iA2-初始化图表开始");
        this.chart = echarts.init(document.getElementById("chart_" + this.id));
        var option = {
          title: {
            text: "",
          },
          legend: {
            data: [],
          },
          tooltip: {
            trigger: "axis",
            axisPointer: {
              type: "shadow",
            },
          },
          grid: {
            top: 70,
            right: 24,
            left: 24,
            bottom: 48,
          },
          yAxis: {
            data: [],
            inverse: true,
            axisLine: { show: false },
            axisTick: { show: false },
            axisLabel: {
              margin: 0,
              fontSize: 14,
            },
            axisPointer: {
              label: {
                show: true,
                margin: 0,
              },
            },
          },
          xAxis: {
            splitLine: { show: false },
            axisLabel: { show: false },
            axisTick: { show: false },
            axisLine: { show: false },
          },
          series: [
            {
              itemStyle: {
                color: "#f7b033",
              },
              name: "噪音",
              type: "pictorialBar",
              label: {
                normal: {
                  // formatter:'{a} </br>{b}:{c}%',
                  formatter: "{c}{title|db}",
                  rich: {
                    title: {
                      color: "#5B92FF",
                      fontSize: "16px",
                      align: "center",
                    },
                  },
                  // color:'#5B92FF',
                  show: true,
                  position: "left,top",
                  offset: [10, -30],
                  textStyle: {
                    fontSize: 50,
                    color: "#fff",
                  },
                  subtextStyle: {
                    color: "#aaa", // 副标题文字颜色
                  },
                },
              },
              symbolRepeat: true,
              symbolSize: ["7%", "50%"],
              barCategoryGap: "0%",
              data: [
                {
                  value: this.noise,
                  symbol: 'media/bg/chart-img.png', //'circle', 'rect', 'roundRect', 'triangle', 'diamond', 'pin', 'arrow', 'none'
                },
              ],
            },
          ],
        };
        this.chart.setOption(option);
        console.log("06-kA6iA2-初始化图表完成");
        const resizeObserver = new ResizeObserver((entries) => {
          this.chart && this.chart.resize();
        });
        resizeObserver.observe(document.getElementById("chart_" + this.id));
      },
    },
  };
</script>
<!--插件调试区域04-->
<style scoped>
  .chart-all-kA6iA2 {
    width: 100%;
    height: 100%;
    /* position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%); */
    /* border: 1px solid rgb(41, 189, 139); */
  }

  .chart-top-kA6iA2 {
    padding-left: 0px;
    left: 0px;
    top: 0px;
    width: 100%;
    height: 20px;
    box-sizing: border-box;
    /* border: 2px solid rgb(24, 222, 50); */
  }

  .chart-body-kA6iA2 {
    width: 100%;
    height: calc(100% - 50px);
    /* border: 2px solid rgb(201, 26, 26); */
  }
</style>