<template>
  <div class="chart-all-fI4bF0">
    <div class="chart-top-fI4bF0">
      <div style="
      text-align:center;
      color: #fff;
      width: 100%;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    ">甲醛浓度当日最高值</div>
      <!-- <div style="color: #5b92ff">Noise concentration</div> -->
    </div>
    <div class="chart-body-fI4bF0" :id="'chart_' + id"></div>
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
        latest: {},
        fields: [],
        chart: null,
        systime: '',
        theTime: '',
        hValue: 0
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
          console.log("01-fI4bF0-图表接收到数据");
          console.log("02-fI4bF0-图表id:" + this.id);
          if (val["fields"]) {
            console.log("03-fI4bF0-fields有值");
            console.log("04-fI4bF0-device_id:" + val["device_id"]);
            this.latest = val["latest"];
            this.fields = val["fields"];
            this.getData();
          } else {
            console.log("05-fI4bF0-fields没有值");
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
    mounted() {
      this.initChart();
      const resizeObserver = new ResizeObserver((entries) => {
        //回调,重置图表大小
        this.chart && this.chart.resize();
      });
      resizeObserver.observe(document.getElementById("chart_" + this.id));
    },
    methods: {
      getData() {
        if (this.latest.systime.substr(0, 10) > this.theTime) {
          this.theTime = this.latest.systime.substr(0, 10)
          console.log(this.theTime)
        }
        for (var i = 0; i < this.fields.length; i++) {
          if (this.theTime == this.fields[i].systime.substr(0, 10)) {
            if (this.hValue < this.fields[i].carbon) {
              this.hValue = this.fields[i].carbon
            }
          }
        }
        this.initChart();
        // setTimeout(() => {
        //   this.initChart();
        // }, 1000);
      },
      initChart() {
        console.log("05-fI4bF0-初始化图表开始");
        this.chart = echarts.init(document.getElementById("chart_" + this.id));
        var option = {
          title: {
            text: ''
          },
          legend: {
            data: []
          },
          tooltip: {
            trigger: 'axis',
            axisPointer: {
              type: 'shadow'
            }
          },
          grid: {
            containLabel: true,
            top: '100px',
            left: 0,
            bottom: '10px',
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
                margin: 0
              }
            }
          },
          xAxis: {
            splitLine: { show: false },
            axisLabel: { show: false },
            axisTick: { show: false },
            axisLine: { show: false }
          },
          series: [{
            itemStyle: {
              color: '#3ECF63',
            },
            name: '甲醛',
            type: 'pictorialBar',
            label: {
              normal: {
                formatter: '{c}{title|mg/m³}',
                rich: {
                  title: {
                    color: '#5B92FF',
                    fontSize: '16px',
                    align: 'center'
                  },
                },
                show: true,
                position: 'left,top',
                offset: [10, -30],
                textStyle: {
                  fontSize: 50
                },
                color: '#fff'
              }
            },
            symbolRepeat: true,
            symbolSize: ['7%', '50%'],
            barCategoryGap: '0%',
            data: [{
              value: this.hValue,
              symbol: 'roundRect',

              // symbol: 'media/bg/chart-img.png',
            }]
          }]
        };
        //this.chart.clear();
        this.chart.setOption(option);
        // window.addEventListener("resize", () => {
        //   this.chart.resize();
        // });
        console.log("06-fI4bF0-初始化图表完成");
      },
      /**
       * 重置图表大小
       */
      resizeChart() {
        /* eslint-disable no-unused-expressions */
        this.chart && this.chart.resize();
      },
    },
  };
</script>
<style scoped>
  .chart-all-fI4bF0 {
    width: 100%;
    height: 100%;
    /* position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%); */
    /* border: 1px solid rgb(41, 189, 139); */
  }

  .chart-top-fI4bF0 {
    padding-left: 0px;
    left: 0px;
    top: 0px;
    width: 100%;
    height: 20px;
    box-sizing: border-box;
    /* border: 2px solid rgb(24, 222, 50); */
  }

  .chart-body-fI4bF0 {
    width: 100%;
    height: calc(100% - 50px);
    /* border: 2px solid rgb(201, 26, 26); */
  }
</style>