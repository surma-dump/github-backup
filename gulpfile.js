'use strict';

var gulp = require('gulp');
var $ = require('gulp-load-plugins')();
var del = require('del');
var runSequence = require('run-sequence');
var browserSync = require('browser-sync');
var reload = browserSync.reload;
var path = require('path');
var pkg = require('./package.json');

gulp.task('jshint', function () {
  return gulp.src('static/**/*.js')
    .pipe(reload({stream: true, once: true}))
    .pipe($.jshint())
    .pipe($.jshint.reporter('jshint-stylish'))
    .pipe($.if(!browserSync.active, $.jshint.reporter('fail')));
});

gulp.task('images', function () {
  return gulp.src('static/**/*.{svg,png,jpg}')
    .pipe($.flatten())
    .pipe($.cache($.imagemin({
      progressive: true,
      interlaced: true
    })))
    .pipe(gulp.dest('./dist/images'))
    .pipe($.size({title: 'images'}));
});

gulp.task('fonts', function () {
  return gulp.src([
    'static/fonts/*'
  ]).pipe(gulp.dest('.tmp/fonts'));
});

gulp.task('styles', function () {
  return gulp.src([
    'static/sass/*.scss'
  ])
    .pipe($.sass({
      precision: 10,
      onError: console.error.bind(console, 'Sass error:')
    }))
    .pipe(gulp.dest('./.tmp/css'))
    .pipe($.csso())
    .pipe(gulp.dest('./dist/css'))
    .pipe($.size({title: 'styles'}));
});

gulp.task('static', function() {
  var assets = $.useref.assets({
    searchPath: ['.tmp', 'static'],
  });

  return gulp.src([
    'static/index.html'
  ])
    .pipe($.vulcanize({
      dest: 'dist'
    }))
    .pipe(assets)
    .pipe($.if('*.js', $.uglify()))
    .pipe($.if('*.css', $.csso()))
    .pipe(assets.restore())
    .pipe($.useref())
    // .pipe($.htmlmin({
    //   removeComments: true,
    //   collapseWhitespace: true,
    //   removeAttributeQuotes: true,
    //   removeRedundantAttributes: true,
    //   removeEmptyAttributes: true,
    //   removeScriptTypeAttributes: true,
    //   removeStyleLinkTypeAttributes: true
    // }))
    .pipe(gulp.dest('./dist'));
})

gulp.task('clean', del.bind(null, ['.tmp', 'dist'], {dot: true}));

gulp.task('default', function(cb) {
  runSequence('clean',
    ['images', 'fonts', 'styles'],
    'static',
    cb);
});

gulp.task('serve', ['default'], function () {
  browserSync({
    notify: false,
    server: {
      baseDir: ['.tmp', 'static'],
      routes: {
        // '/components/fonts': 'fonts'
      }
    }
  });

  gulp.watch(['static/**/*.{js,html}'], ['jshint', reload]);
  gulp.watch(['static/**/*.{scss,css}'], ['styles', reload]);
  gulp.watch(['static/**/*.{svg,png,jpg}'], ['images', reload]);
});
