<template>
  <!-- 随机密码生成：https://c.runoob.com/front-end/686/ -->
  <!-- 所有class后必须跟随机密码，打开上面网页勾掉特殊字符生成随机密码如下kA6iA2 -->
  <div class="chart-out-oK4dN9">
    <!-- 总盒子（总盒子样式勿要修改） -->
    <div class="chart-all-oK4dN9">
      <!-- 标题盒子（每个图表必须有标题）格式和样式请勿修改 -->
      <div class="chart-top-oK4dN9">
        <div style="
            text-align:center;
            color: #fff;
            width: 100%;;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
          ">
          湿度曲线
        </div>
      </div>
      <!-- 图表主盒子（主盒子样式勿要修改） -->
      <div class="chart-body-oK4dN9" :id="'chart_' + id"></div>
    </div>
  </div>
</template>
<script>
  //import * as echarts from "echarts";
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
        oneData: [],
        sysTimeData: [],
        chart: null,
        flag: 0,
        mytime: null,
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
          console.log("oK4dN9 接收到apiData");
          console.log("oK4dN9 id:" + this.id);
          if (val["fields"]) {
            console.log("oK4dN9 fields有值:");
            console.log(
              "oK4dN9 device_id:" + val["device_id"]
            );
            this.latest = val["latest"];
            this.fields = val["fields"];
            this.getData();
          } else {
            console.log("oK4dN9 fields没有值:");
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
    methods: {
      // 插件调试区域03
      getData() {
        console.log("oK4dN9 进入apiData处理");
        // 最后再刷新数据
        //判断是否是第一次请求来的数据
        if (this.flag == 0) {
          console.log("oK4dN9 第一次apiData处理开始");
          this.flag = 1;
          //遍历数据字典，获取曲线数据
          for (var i = 0; i < this.fields.length; i++) {
            var item = this.fields[i];
            var d = new Date(item["systime"]);
            if (d >= this.mytime) {
              this.mytime = d.setMinutes(d.getMinutes() + 1);
              this.oneData.push(item["hum"]);
              this.sysTimeData.push(item["systime"].slice(11, 16));
            }
          }
          console.log("oK4dN9 第一次apiData处理完成");
        } else {
          console.log("oK4dN9 apiData后续处理");
          //遍历数据字典，获取曲线数据
          for (var i = 0; i < this.fields.length; i++) {
            var item = this.fields[i];
            var d = new Date(item["systime"]);
            if (d >= this.mytime) {
              console.log(this.mytime);
              this.mytime = d.setMinutes(d.getMinutes() + 1);
              this.oneData.push(item["hum"]);
              this.sysTimeData.push(item["systime"].slice(11, 16));
            }
          }
        }
        setTimeout(() => {
          this.initChart();
        }, 1000);
      },
      initChart() {
        console.log("05-oK4dN9-初始化图表开始");
        this.chart = echarts.init(document.getElementById("chart_" + this.id));
        var option = {
          title: {
            show: false,
            text: "曲线图",
            textStyle: {
              align: "center",
              verticalAlign: "middle",
            },
            top: 10,
            left: "10",
          },
          legend: {
            show: true,
            top: 10,
            textStyle: {
              color: "#fff",
            },
            // data: [],
          },
          tooltip: {
            trigger: "axis",
            axisPointer: {
              type: "cross",
              label: {
                backgroundColor: "#6a7985",
              },
            },
          },
          grid: {
            top: "15%",
            right: "2%",
            left: "5%",
            bottom: "10%",
          },
          xAxis: [
            {
              type: "category",
              boundaryGap: false,
              axisLabel: {
                color: "#88adf6",
              },
              axisLine: {
                show: true,
                lineStyle: {
                  color: "#0f2486",
                },
              },
              axisTick: {
                show: true,
              },
              splitLine: {
                show: false,
                lineStyle: {
                  color: "#0f2486",
                },
              },
              data: this.sysTimeData,
            },
          ],
          yAxis: [
            {
              type: "value",
              nameTextStyle: {
                color: "#88adf6",
              },
              /*min: -40,
                              max: 45,*/
              axisLabel: {
                formatter: "{value}",
                textStyle: {
                  color: "#88adf6",
                },
              },
              axisLine: {
                show: true,
                lineStyle: {
                  color: "#0f2486",
                },
              },
              axisTick: {
                show: false,
              },
              splitLine: {
                show: false,
              },
            },
          ],
          series: [
            {
              name: "湿度",
              type: "line",
              smooth: true,
              stack: "",
              symbol: "emptyCircle",
              symbolSize: 6,
              itemStyle: {
                normal: {
                  color: {
                    type: "linear",
                    x: 0,
                    y: 0,
                    x2: 1,
                    y2: 0,
                    colorStops: [
                      {
                        offset: 0,
                        color: "#427DE1", // 0%
                      },
                      {
                        offset: 1,
                        color: "#21EDFA", // 100%
                      },
                    ],
                  },
                  lineStyle: {
                    width: 2,
                  },
                },
              },
              markPoint: {
                itemStyle: {
                  normal: {
                    color: "#fff",
                  },
                },
              },
              data: this.oneData,
            },
          ],
        };
        this.chart.setOption(option);
        console.log("06-oK4dN9-初始化图表完成");
        const resizeObserver = new ResizeObserver((entries) => {
          this.chart && this.chart.resize();
        });
        resizeObserver.observe(document.getElementById("chart_" + this.id));
      },
    },
  };
</script>
<style scoped>
  .chart-out-oK4dN9 {
    width: 100%;
    height: 100%;
    position: relative;
  }

  /* 请勿修改chart-all */
  .chart-all-oK4dN9 {
    width: 100%;
    height: 100%;
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    /* border: 1px solid rgb(41, 189, 139); */
  }

  /* 请勿修改chart-top */
  .chart-top-oK4dN9 {
    padding-left: 0px;
    left: 0px;
    top: 0px;
    width: 100%;
    height: 20px;
    box-sizing: border-box;
    /* border: 2px solid rgb(24, 222, 50); */
  }

  /* 请勿修改chart-body */
  .chart-body-oK4dN9 {
    width: 100%;
    height: calc(100% - 50px);
    /* border: 2px solid rgb(201, 26, 26); */
  }
</style>